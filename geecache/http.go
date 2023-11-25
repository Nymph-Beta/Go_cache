package geecache

import (
	"encoding/json"
	"fmt"
	"geecache/consistenthash"

	"bytes"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
)

const (
	defaultBasePath = "/"
	defaultReplicas = 50
)

// HTTPPool implements PeerPicker for a pool of HTTP peers.
type HTTPPool struct {
	// this peer's base URL, e.g. "https://example.net:8000"
	self        string
	basePath    string
	mu          sync.Mutex // guards peers and httpGetters
	peers       *consistenthash.Map
	httpGetters map[string]*httpGetter // keyed by e.g. "http://10.0.0.2:8008"
}

// NewHTTPPool initializes an HTTP pool of peers.
func NewHTTPPool(self string) *HTTPPool {
	return &HTTPPool{
		self:     self,
		basePath: defaultBasePath,
	}
}

// Log info with server name
func (p *HTTPPool) Log(format string, v ...interface{}) {
	log.Printf("[Server %s] %s", p.self, fmt.Sprintf(format, v...))
}

// ServeHTTP handle all http requests
func (p *HTTPPool) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	local := r.URL.Query().Get("local") == "true"
	hostPort := r.Host // 返回如 "localhost:8001"
	_, port, err := net.SplitHostPort(hostPort)
	if err != nil {
		// handle error
	}
	fmt.Println(port) // 这里会打印 "8001"
	if !strings.HasPrefix(r.URL.Path, p.basePath) {
		panic("HTTPPool serving unexpected path: " + r.URL.Path)
	}
	p.Log("%s %s", r.Method, r.URL.Path)
	parts := strings.SplitN(r.URL.Path[len(p.basePath):], "/", 2)
	switch r.Method {
	case "GET":
		if len(parts) < 1 || len(parts) > 2 {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		groupName := "scores"
		var key string
		//key := parts[1]
		if len(parts) == 1 {
			key = parts[0]
		} else {
			groupName = parts[0]
			key = parts[1]
		}
		group := GetGroup("scores")
		if group == nil {
			http.Error(w, "no such group: "+groupName, http.StatusNotFound)
			return
		}
		view, err, _ := group.Get(key, port, local)
		if err != nil {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}
		body, err := json.Marshal(map[string]string{key: string(view.ByteSlice())})
		fmt.Println("JSON Body:", body)

		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Write(body)

	case "POST":
		body, _ := ioutil.ReadAll(r.Body)
		log.Printf("[HTTPPool] Received POST request with body: %s", string(body)) // 添加此日志

		var data map[string]interface{}
		if err := json.Unmarshal(body, &data); err != nil {
			log.Printf("[HTTPPool] Error unmarshalling request body: %v", err) // 添加此日志
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		for key, val := range data {
			log.Printf("[HTTPPool] Processing key: %s, value: %v", key, val)
			strVal, ok := val.(string)
			if !ok {
				// If it is not  a string, attempt to convert it to a JSON string.
				jsonVal, err := json.Marshal(val)
				if err != nil {
					log.Printf("[HTTPPool] Error marshalling value for key %s: %v", key, err) // 添加此日志
					http.Error(w, "bad request", http.StatusBadRequest)
					return
				}
				strVal = string(jsonVal)
			}

			group := GetGroup("scores")
			jsonData := string(body)

			group.Add(key, ByteView{b: []byte(strVal)}, port, local, jsonData)
		}

		w.WriteHeader(http.StatusOK)

	case "DELETE":
		if len(parts) < 1 || len(parts) > 2 {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		groupName := "scores"
		var key string
		if len(parts) == 1 {
			key = parts[0]
		} else {
			groupName = parts[0]
			key = parts[1]
		}
		group := GetGroup("scores")
		if group == nil {
			http.Error(w, "no such group:"+groupName, http.StatusNotFound)
			return
		}

		deletedCount := group.Delete(key, port, local)

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(strconv.Itoa(deletedCount)))
	default:
		http.Error(w, "not supported", http.StatusMethodNotAllowed)
	}
}

// Set updates the pool's list of peers.
func (p *HTTPPool) Set(peers ...string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	log.Printf("[HTTPPool] Setting peers: %v", peers)

	p.peers = consistenthash.New(defaultReplicas, nil)
	p.peers.Add(peers...)

	p.httpGetters = make(map[string]*httpGetter, len(peers))
	for _, peer := range peers {
		p.httpGetters[peer] = &httpGetter{baseURL: peer + p.basePath}
		log.Printf("[HTTPPool] Created httpGetter for peer: %s with baseURL: %s", peer, peer+p.basePath)
	}
}

// PickPeer picks a peer according to key
func (p *HTTPPool) PickPeer(peeraddr string) (PeerGetter, string, string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if peer, nowport := p.peers.Get(peeraddr); peer != "" && peer != p.self {
		p.Log("Pick peer %s", peer)
		//nowpeer := &httpGetter{baseURL: peer}
		return p.httpGetters[peer], nowport, peer
	}
	return nil, "", ""
}

var _ PeerPicker = (*HTTPPool)(nil)

type httpGetter struct {
	baseURL string
}

func (h *httpGetter) Get(in *Request, out *Response) error {
	u := fmt.Sprintf(
		//"%v%v/%v",
		"%v%v/%v?local=true",
		h.baseURL,
		url.QueryEscape(in.Group),
		url.QueryEscape(in.Key),
	)
	res, err := http.Get(u)
	fmt.Printf("Response: %+v, Error: %v\n", res, err)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned: %v", res.Status)
	}

	bytes, err := ioutil.ReadAll(res.Body)
	fmt.Println("Body content:", string(bytes))
	if err != nil {
		return fmt.Errorf("reading response body: %v", err)
	}

	var data map[string]string
	if err = json.Unmarshal(bytes, &data); err != nil {
		return fmt.Errorf("decoding response body: %v", err)
	}
	value, exists := data[in.Key]
	if !exists {
		return fmt.Errorf("key '%s' not found in the response", in.Key)
	}
	fmt.Printf("Extracted value for key '%s': %s\n", in.Key, value)
	out.Value = []byte(value)

	return nil
}

func (h *httpGetter) Delete(in *Request) bool {
	u := fmt.Sprintf(
		"%v%v/%v?local=true",
		h.baseURL,
		url.QueryEscape(in.Group),
		url.QueryEscape(in.Key),
	)

	log.Printf("now url is %s", u)
	req, err := http.NewRequest(http.MethodDelete, u, nil)
	fmt.Printf("REQ: %+v, Error: %v\n", req, err)
	if err != nil {
		return false
	}
	res, err := http.DefaultClient.Do(req)
	fmt.Printf("Response: %+v, Error: %v\n", res, err)
	bytes, err := ioutil.ReadAll(res.Body)
	fmt.Println("Body content:", string(bytes))
	deleteres := string(bytes)
	if err != nil {
		return false
	}
	defer res.Body.Close()
	log.Printf("Response status code: %d, expected: %d", res.StatusCode, http.StatusOK)
	return deleteres == "1"
}

func (h *httpGetter) Update(in *Request, data string) error {
	u := fmt.Sprintf("%v", h.baseURL)
	req, err := http.NewRequest(http.MethodPost, u, bytes.NewBufferString(data))
	if err != nil {
		log.Printf("Error creating HTTP request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Fatalf("Error sending request: %v", err)
	}
	defer resp.Body.Close()
	return err
}

var _ PeerGetter = (*httpGetter)(nil)
