package main

/*
$ curl "http://localhost:9999/api?key=Tom"
630

$ curl "http://localhost:9999/api?key=kkk"
kkk not exist
*/

import (
	"encoding/json"
	"flag"
	"fmt"
	"geecache"
	"log"
	"net/http"
)

var db = map[string]string{
	"Tom":  "630",
	"Jack": "589",
	"Sam":  "567",
}

func createGroup() *geecache.Group {
	return geecache.NewGroup("scores", 2<<10, geecache.GetterFunc(
		func(key string) ([]byte, error) {
			log.Println("[SlowDB] search key", key)
			if v, ok := db[key]; ok {
				log.Println("[SlowDB] Found value:", v) // <-- 添加这一行
				return []byte(v), nil
			}
			return nil, fmt.Errorf("%s not exist", key)
		}))
}

func startCacheServer(addr string, addrs []string, gee *geecache.Group) {
	peers := geecache.NewHTTPPool(addr)
	peers.Set(addrs...)
	gee.RegisterPeers(peers)
	http.HandleFunc("/add", func(w http.ResponseWriter, r *http.Request) {
		var data map[string]string
		json.NewDecoder(r.Body).Decode(&data)
		for k, v := range data {
			gee.Set(k, geecache.NewByteView([]byte(v)))

		}
		w.WriteHeader(http.StatusOK)
	})
	http.HandleFunc("/delete", func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Query().Get("key")
		gee.Delete(key)
		w.WriteHeader(http.StatusOK)
	})

	log.Println("geecache is running at", addr)
	log.Fatal(http.ListenAndServe(addr[7:], peers))
}

func startAPIServer(apiAddr string, gee *geecache.Group) {
	http.Handle("/api", http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			key := r.URL.Query().Get("key")
			view, err := gee.Get(key)
			if err != nil {
				log.Printf("Error retrieving key %s: %v", key, err) // 添加此日志
				//http.Error(w, err.Error(), http.StatusInternalServerError)
				http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
				return
			}
			log.Printf("Retrieved key %s: %s", key, view.String()) // 添加此日志
			// w.Header().Set("Content-Type", "application/octet-stream")
			// w.Write(view.ByteSlice())
			w.Header().Set("Content-Type", "application/json")
			jsonResponse := fmt.Sprintf(`{"key":"%s","value":"%s"}`, key, view.String())
			w.Write([]byte(jsonResponse))
			log.Printf("Data written to response for key %s", key) // 添加此日志

		}))
	log.Println("fontend server is running at", apiAddr)
	log.Fatal(http.ListenAndServe(apiAddr[7:], nil))

}

func main() {
	var port int
	var api bool
	flag.IntVar(&port, "port", 8001, "Geecache server port")
	flag.BoolVar(&api, "api", false, "Start a api server?")
	flag.Parse()

	apiAddr := "http://localhost:9999"
	addrMap := map[int]string{
		8001: "http://localhost:8001",
		8002: "http://localhost:8002",
		8003: "http://localhost:8003",
	}

	var addrs []string
	for _, v := range addrMap {
		addrs = append(addrs, v)
	}

	gee := createGroup()
	if api {
		go startAPIServer(apiAddr, gee)
	}
	startCacheServer(addrMap[port], addrs, gee)
}
