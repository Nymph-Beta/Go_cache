package geecache

// PeerPicker is the interface that must be implemented to locate
// the peer that owns a specific key.
type PeerPicker interface {
	PickPeer(key string) (peer PeerGetter, nowport string, nowpeer string)
}

// PeerGetter is the interface that must be implemented by a peer.
type PeerGetter interface {
	Get(in *Request, out *Response) error
	Delete(in *Request) bool
	Update(in *Request, data string) error
}
