package nodes

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/danmuck/dps_files/src/api/ledgers"
	"github.com/danmuck/dps_files/src/api/transport"
	"github.com/danmuck/dps_files/src/key_store"
	logs "github.com/danmuck/smplog"
)

// DefaultServerNode embeds DefaultNode and adds file storage via FileLedger,
// RPC dispatch, and optional HTTP serving.
type DefaultServerNode struct {
	*DefaultNode
	storage    ledgers.FileLedger
	httpAddr   string
	httpServer *http.Server
	mux        *http.ServeMux
}

// ServerOption configures optional DefaultServerNode features.
type ServerOption func(*DefaultServerNode)

// WithHTTP enables an HTTP server on the given address.
func WithHTTP(addr string) ServerOption {
	return func(s *DefaultServerNode) { s.httpAddr = addr }
}

// NewServerNode creates a DefaultServerNode backed by a KeyStore at storageDir.
func NewServerNode(id []byte, addr string, storageDir string, opts ...ServerOption) (*DefaultServerNode, error) {
	base, err := NewDefaultNode(id, addr)
	if err != nil {
		return nil, err
	}

	ks, err := key_store.InitKeyStore(storageDir)
	if err != nil {
		return nil, fmt.Errorf("init keystore: %w", err)
	}

	sn := &DefaultServerNode{
		DefaultNode: base,
		storage:     key_store.NewFileLedger(ks),
		mux:         http.NewServeMux(),
	}
	sn.registerHTTPRoutes()
	for _, opt := range opts {
		opt(sn)
	}
	return sn, nil
}

// Storage returns the node's FileLedger.
func (s *DefaultServerNode) Storage() ledgers.FileLedger {
	return s.storage
}

// Start begins the TCP listener, RPC dispatch loop, and optional HTTP server.
// It does NOT call DefaultNode.Start() to avoid a duplicate RPC consumer.
func (s *DefaultServerNode) Start() error {
	if err := s.TCPHandler.ListenAndAccept(); err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	go s.dispatchRPCs()

	if s.httpAddr != "" {
		s.httpServer = &http.Server{Addr: s.httpAddr, Handler: s.mux}
		go func() {
			if err := s.httpServer.ListenAndServe(); err != http.ErrServerClosed {
				logs.Warnf("HTTP server error: %v", err)
			}
		}()
	}
	return nil
}

// dispatchRPCs reads inbound RPCs and routes them through HandleRPC.
func (s *DefaultServerNode) dispatchRPCs() {
	ch := s.TCPHandler.ProcessRPC()
	for {
		select {
		case <-s.exit:
			return
		case rpc := <-ch:
			if rpc == nil {
				continue
			}
			resp, err := s.HandleRPC(rpc)
			if err != nil {
				logs.Warnf("HandleRPC error: %v", err)
				continue
			}
			if resp != nil && rpc.Sender != nil {
				conn, dialErr := s.TCPHandler.Dial(rpc.Sender.Address)
				if dialErr != nil {
					logs.Warnf("dial back to %s: %v", rpc.Sender.Address, dialErr)
					continue
				}
				if sendErr := s.TCPHandler.Send(conn, resp); sendErr != nil {
					logs.Warnf("send response to %s: %v", rpc.Sender.Address, sendErr)
				}
			}
		}
	}
}

// HandleRPC processes a single RPC and returns a response.
func (s *DefaultServerNode) HandleRPC(rpc *transport.RPC) (*transport.RPC, error) {
	switch rpc.Meta.Command {
	case transport.Command_PING:
		return &transport.RPC{
			Meta:   &transport.RPCT{Command: transport.Command_ACK},
			Sender: s.nodeInfo(),
		}, nil

	case transport.Command_LIST:
		summaries := s.storage.ListKnownFilesMetadata()
		data, err := json.Marshal(summaries)
		if err != nil {
			return nil, fmt.Errorf("marshal file list: %w", err)
		}
		return &transport.RPC{
			Meta:    &transport.RPCT{Command: transport.Command_ACK},
			Sender:  s.nodeInfo(),
			Payload: data,
		}, nil

	case transport.Command_UPLOAD:
		name := string(rpc.Key)
		fid, err := s.storage.StoreFileLocal(name, rpc.Value)
		if err != nil {
			return nil, fmt.Errorf("store file: %w", err)
		}
		return &transport.RPC{
			Meta:   &transport.RPCT{Command: transport.Command_ACK},
			Sender: s.nodeInfo(),
			Key:    fid[:],
		}, nil

	case transport.Command_DOWNLOAD:
		if len(rpc.Key) != 32 {
			return nil, fmt.Errorf("DOWNLOAD requires 32-byte file hash key")
		}
		var fid ledgers.FileID
		copy(fid[:], rpc.Key)
		data, err := s.storage.ReassembleFileToBytes(fid)
		if err != nil {
			return nil, fmt.Errorf("reassemble file: %w", err)
		}
		return &transport.RPC{
			Meta:   &transport.RPCT{Command: transport.Command_ACK},
			Sender: s.nodeInfo(),
			Value:  data,
		}, nil

	case transport.Command_DELETE:
		if len(rpc.Key) != 32 {
			return nil, fmt.Errorf("DELETE requires 32-byte file hash key")
		}
		var fid ledgers.FileID
		copy(fid[:], rpc.Key)
		if err := s.storage.DeleteFile(fid); err != nil {
			return nil, fmt.Errorf("delete file: %w", err)
		}
		return &transport.RPC{
			Meta:   &transport.RPCT{Command: transport.Command_ACK},
			Sender: s.nodeInfo(),
		}, nil

	default:
		return nil, fmt.Errorf("unhandled command: %v", rpc.Meta.Command)
	}
}

// ServeHTTP delegates to the internal ServeMux.
func (s *DefaultServerNode) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// Shutdown stops the HTTP server (if running) and the base node.
func (s *DefaultServerNode) Shutdown() error {
	if s.httpServer != nil {
		s.httpServer.Close()
	}
	return s.DefaultNode.Shutdown()
}

// nodeInfo returns a pointer to the node's transport.NodeInfo.
func (s *DefaultServerNode) nodeInfo() *transport.NodeInfo {
	info := s.NodeInfo()
	return &info
}
