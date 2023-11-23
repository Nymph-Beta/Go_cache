package geecache

import (
	"encoding/json"
	"fmt"
	"geecache/consistenthash"

	//pb "geecache/geecachepb"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	//"github.com/golang/protobuf/proto"
)

const (
	defaultBasePath = "/_geecache/"
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
		// self:     self,
		// basePath: defaultBasePath,
		self:     self,
		basePath: "/",
	}
}

// Log info with server name
func (p *HTTPPool) Log(format string, v ...interface{}) {
	log.Printf("[Server %s] %s", p.self, fmt.Sprintf(format, v...))
}

func (p *HTTPPool) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost && r.URL.Path == "/" {
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
				// 如果不是字符串，尝试将其转换为JSON字符串
				jsonVal, err := json.Marshal(val)
				if err != nil {
					log.Printf("[HTTPPool] Error marshalling value for key %s: %v", key, err) // 添加此日志
					http.Error(w, "bad request", http.StatusBadRequest)
					return
				}
				strVal = string(jsonVal)
			}
			group := GetGroup("scores") // 默认组名
			if group == nil {
				log.Printf("[HTTPPool] No such group: scores") // 添加此日志
				http.Error(w, "no such group: scores", http.StatusNotFound)
				return
			}
			log.Printf("[HTTPPool] GetGroup returned: %+v", group) // 打印group的值
			group.populateCache(key, ByteView{b: []byte(strVal)})
			log.Printf("[HTTPPool] Cached key: %s, value: %s", key, strVal) // 添加此日志
		}
		w.WriteHeader(http.StatusOK)
		return
	}

	if !strings.HasPrefix(r.URL.Path, p.basePath) {
		http.Error(w, "unexpected path: "+r.URL.Path, http.StatusBadRequest)
		return
	}
	p.Log("%s %s", r.Method, r.URL.Path)

	switch r.Method {
	case http.MethodGet:
		key := r.URL.Path[len(p.basePath):]
		group := GetGroup("scores") // 默认组名
		if group == nil {
			http.Error(w, "no such group: scores", http.StatusNotFound)
			return
		}
		view, err := group.Get(key)
		if err != nil {
			log.Printf("[HTTPPool.ServeHTTP] Error getting data for key %s: %v", key, err)
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		if view.Len() == 0 {
			log.Printf("[HTTPPool.ServeHTTP] Data for key %s is empty", key)
			http.Error(w, "Data is empty", http.StatusNotFound)
			return
		}
		response, _ := json.Marshal(map[string]string{key: view.String()})
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Write(response)

	case http.MethodPost:
		body, _ := ioutil.ReadAll(r.Body)
		var data map[string]string
		if err := json.Unmarshal(body, &data); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		for key, val := range data {
			group := GetGroup("scores") // 默认组名
			if group == nil {
				http.Error(w, "no such group: scores", http.StatusNotFound)
				return
			}
			group.populateCache(key, ByteView{b: []byte(val)})
		}
		w.WriteHeader(http.StatusOK)

	case http.MethodDelete:
		key := r.URL.Path[len(p.basePath):]
		group := GetGroup("scores") // 默认组名
		if group == nil {
			http.Error(w, "no such group: scores", http.StatusNotFound)
			return
		}
		err := group.mainCache.remove(key)
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("1"))

	default:
		http.Error(w, "bad method", http.StatusMethodNotAllowed)
	}
}

// Set updates the pool's list of peers.
func (p *HTTPPool) Set(peers ...string) {
	log.Printf("[HTTPPool] Setting peers: %v", peers)
	p.mu.Lock()
	defer p.mu.Unlock()
	p.peers = consistenthash.New(defaultReplicas, nil)
	p.peers.Add(peers...)
	p.httpGetters = make(map[string]*httpGetter, len(peers))
	for _, peer := range peers {
		p.httpGetters[peer] = &httpGetter{baseURL: peer + p.basePath}
	}
	for peer, getter := range p.httpGetters {
		log.Printf("[HTTPPool] Peer: %s, Getter: %v", peer, getter)
	}

}

// PickPeer picks a peer according to key
func (p *HTTPPool) PickPeer(key string) (PeerGetter, bool) {
	log.Printf("[PeerPicker] Picking peer for key: %s", key) // 添加此日志
	p.mu.Lock()
	defer p.mu.Unlock()
	if peer := p.peers.Get(key); peer != "" && peer != p.self {
		p.Log("Pick peer %s", peer)
		return p.httpGetters[peer], true
	}
	log.Printf("[PeerPicker] No peer picked for key: %s", key) // 添加此日志
	return nil, false
}

var _ PeerPicker = (*HTTPPool)(nil)

type httpGetter struct {
	baseURL string
}

func (h *httpGetter) Get(in *JsonRequest, out *JsonResponse) error {
	log.Printf("[PeerGetter] Getting data for key: %s", in.Key) // 添加此日志
	u := fmt.Sprintf(
		"%v%v",
		h.baseURL,
		url.QueryEscape(in.Key))
	res, err := http.Get(u)
	if err != nil {
		log.Printf("[PeerGetter] Error retrieving key %s: %v", in.Key, err)
		return err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned: %v", res.Status)
	}

	bytes, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("reading response body: %v", err)
	}

	if err = json.Unmarshal(bytes, out); err != nil {
		return fmt.Errorf("decoding response body: %v", err)
	}
	log.Printf("[PeerGetter] Retrieved data for key %s: %s", in.Key, out.Value) // 添加此日志
	return nil
}

var _ PeerGetter = (*httpGetter)(nil)
