package geecache

import (
	"errors"
	"fmt"
	"geecache/singleflight"
	"log"
	"strconv"

	//"strings"
	"sync"
)

var addrMap = map[int]string{
	9527: "http://cache-server-1:9527",
	9528: "http://cache-server-2:9528",
	9529: "http://cache-server-3:9529",
}

type Request struct {
	Group string
	Key   string
}

type Response struct {
	Value []byte
}

// A Group is a cache namespace and associated data loaded spread over
type Group struct {
	name      string
	getter    Getter
	mainCache cache
	peers     PeerPicker
	// use singleflight.Group to make sure that each key is only fetched once
	loader *singleflight.Group
	// Determine if the most recent Get was a local get
	localGet bool
}

// A Getter loads data for a key.
type Getter interface {
	Get(key string) ([]byte, error)
}

// A GetterFunc implements Getter with a function.
type GetterFunc func(key string) ([]byte, error)

// Get implements Getter interface function
func (f GetterFunc) Get(key string) ([]byte, error) {
	return f(key)
}

var (
	mu     sync.RWMutex
	groups = make(map[string]*Group)
)

// NewGroup create a new instance of Group
func NewGroup(name string, cacheBytes int64, getter Getter) *Group {
	if getter == nil {
		panic("nil Getter")
	}
	mu.Lock()
	defer mu.Unlock()
	g := &Group{
		name:      name,
		getter:    getter,
		mainCache: cache{cacheBytes: cacheBytes},
		loader:    &singleflight.Group{},
	}
	groups[name] = g
	return g
}

// GetGroup returns the named group previously created with NewGroup, or
// nil if there's no such group.
func GetGroup(name string) *Group {
	mu.RLock()
	g := groups[name]
	mu.RUnlock()
	return g
}

// RegisterPeers registers a PeerPicker for choosing remote peer
func (g *Group) RegisterPeers(peers PeerPicker) {
	if g.peers != nil {
		panic("RegisterPeerPicker called more than once")
	}
	g.peers = peers
}

// Get value for a key from cache
// If the key is not in the cache, use load to find in other port
func (g *Group) Get(key string, port string, loacl bool) (ByteView, error, string) {
	if key == "" {
		return ByteView{}, fmt.Errorf("key is required"), port
	}

	if v, ok := g.mainCache.get(key); ok {
		g.localGet = true
		//log.Println("[GeeCache] hit")
		log.Println("[GeeCache] hit", "Key:", key, "Value:", v)
		return v, nil, port
	}

	// After this, the commands are not issued. Change localGet to false.
	g.localGet = false
	log.Println("load begining")
	return g.Getload(key, port, loacl)
}

// Delete a key from local cache
// If the key is not in the cache, use Deleteload to find in other port
func (g *Group) Delete(key string, port string, local bool) int {
	if g.mainCache.lru == nil {
		return 0
	}
	if deleted := g.mainCache.remove(key); deleted != 0 {
		return 1
	}
	deletedCount := 0
	log.Printf("deletedCount is %d, local is %t", deletedCount, local)
	if deletedCount == 0 && !local {
		log.Println("deleteload begining")
		deletedCount = g.Deleteload(key, port, local)
	}

	return deletedCount
}

// Check if the key is cacahed on other ports before Add
// Use Updateload to upadte the key if the key is cache on other ports
func (g *Group) Add(key string, value ByteView, port string, local bool, jsonData string) {
	log.Printf("get from other peer to make sure")
	_, err, portnum := g.Get(key, port, local)

	//the key is found on other ports.
	if err == nil && !g.localGet {
		// Change localGet to true, Prevent program deadlock
		g.localGet = true
		log.Printf("the key has already existed and Upadate value in this port : %s", portnum)
		err := g.Updateload(portnum, jsonData)
		if err != nil {
			return
		}
		//group.Add(key, ByteView{b: []byte(strVal)})
		return
	}
	g.mainCache.add(key, value)
}

// Get key from other ports
// If there is a local identifier, Getload is not executed
// the local identifier indicates that the request was sent from another port.
func (g *Group) Getload(key string, port string, local bool) (value ByteView, err error, portnum string) {
	var ErrNotFound = errors.New("key not found")
	if local {
		return ByteView{}, ErrNotFound, port
	}

	if g.peers != nil {
		for _, peeraddr := range addrMap {
			// Converts string to int
			portInt, err := strconv.Atoi(port)
			if addrMap[portInt] == peeraddr {
				fmt.Println("Port:", port, "Mapped Address:", addrMap[portInt])
				continue
			}
			_, nowport, nowpeer := g.peers.PickPeer(peeraddr)
			log.Printf("Selected another peer: %s, port: %s", nowpeer, nowport)
			peer := &httpGetter{baseURL: nowpeer}
			if value, err = g.getFromPeer(peer, key); err == nil {
				//portnum := port
				log.Printf("Get key in %s", nowport)
				return value, nil, nowport
			}
			log.Println("[GeeCache] Failed to get from peer", err)
		}

	}

	return g.getLocally(key, port)
}

// Delete key from other ports
// If there is a local identifier, Deleteload is not executed
// the local identifier indicates that the request was sent from another port.
func (g *Group) Deleteload(key string, port string, local bool) int {
	if local {
		return 0
	}
	log.Printf("delete from other peer")
	if g.peers != nil {
		for _, peeraddr := range addrMap {
			portInt, err := strconv.Atoi(port)
			if err != nil {
				log.Printf("Error converting port '%s': %v", port, err)
				continue // 跳过当前迭代
			}
			//fmt.Println("Port:", port, "Mapped Address:", addrMap[portInt])
			if addrMap[portInt] == peeraddr {
				fmt.Println("Port:", port, "Mapped Address:", addrMap[portInt])
				continue
			}

			_, nowport, nowpeer := g.peers.PickPeer(peeraddr)
			log.Printf("Selected another peer: %s, port: %s", nowpeer, nowport)
			peer := &httpGetter{baseURL: nowpeer}
			log.Println("Selected another peer:", peer)
			if deleted := g.deleFromPeer(peer, key); deleted {
				return 1
			}
		}
	}
	return 0
}

// Update keys on other ports
func (g *Group) Updateload(port string, jsondate string) error {
	portInt, _ := strconv.Atoi(port)
	peeraddr := addrMap[portInt]
	peer := &httpGetter{baseURL: peeraddr}
	err := g.updateToPeer(peer, jsondate)
	return err
}

/**
 * The methods getFromPeer, deleFromPeer, and updateToPeer leverage functions get, delete,
 * and update defined in the PeerGetter interface.
 * These methods facilitate GET, DELETE, and UPDATE operations on a peer node within the distributed network architecture.
 */

func (g *Group) getFromPeer(peer PeerGetter, key string) (ByteView, error) {
	log.Println("getfrompeer Selected peer:", peer)
	req := &Request{
		Group: g.name,
		Key:   key,
	}
	res := &Response{}
	err := peer.Get(req, res)
	if err != nil {
		return ByteView{}, err
	}
	return ByteView{b: res.Value}, nil
}

func (g *Group) deleFromPeer(peer PeerGetter, key string) bool {
	log.Println("getfrompeer Selected peer:", peer)
	req := &Request{
		Group: g.name,
		Key:   key,
	}
	return peer.Delete(req)
}

func (g *Group) updateToPeer(peer PeerGetter, data string) error {
	req := &Request{}
	return peer.Update(req, data)
}

// Search in locally configured database
func (g *Group) getLocally(key string, port string) (ByteView, error, string) {
	log.Printf("From %s getting local", port)
	bytes, err := g.getter.Get(key)
	if err != nil {
		return ByteView{}, err, port

	}
	value := ByteView{b: cloneBytes(bytes)}
	g.populateCache(key, value)
	return value, nil, port
}

func (g *Group) populateCache(key string, value ByteView) {
	g.mainCache.add(key, value)
}
