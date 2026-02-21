package nodes

import (
	"fmt"
	"net"

	grpcserver "github.com/danmuck/dps_files/src/api/grpc"
	"github.com/danmuck/dps_files/src/api/ledgers"
	"github.com/danmuck/dps_files/src/api/pb"
	"github.com/danmuck/dps_files/src/key_store"
	logs "github.com/danmuck/smplog"
	"google.golang.org/grpc"
)

// DefaultServerNode serves files over gRPC. An optional gRPC-Gateway HTTP
// listener can be added with WithHTTP to expose a REST/JSON API on a second port.
type DefaultServerNode struct {
	*DefaultNode
	storage    ledgers.FileLedger
	grpcServer *grpc.Server
	httpAddr   string
	listener   net.Listener
}

// ServerOption configures optional DefaultServerNode features.
type ServerOption func(*DefaultServerNode)

// WithHTTP enables a gRPC-Gateway HTTP listener on the given address.
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
		grpcServer:  grpc.NewServer(),
	}
	pb.RegisterDPSFilesServer(sn.grpcServer, grpcserver.New(sn.storage))
	for _, opt := range opts {
		opt(sn)
	}
	return sn, nil
}

// Storage returns the node's FileLedger.
func (s *DefaultServerNode) Storage() ledgers.FileLedger {
	return s.storage
}

// RawKeyStore returns the underlying *KeyStore for TUI operations.
func (s *DefaultServerNode) RawKeyStore() *key_store.KeyStore {
	if ksl, ok := s.storage.(*key_store.KeyStoreLedger); ok {
		return ksl.KeyStore()
	}
	return nil
}

// Addr returns the address the gRPC listener is bound to.
func (s *DefaultServerNode) Addr() string {
	if s.listener != nil {
		return s.listener.Addr().String()
	}
	return s.address
}

// Start binds the TCP listener, starts serving gRPC, and optionally starts the
// gRPC-Gateway HTTP listener.
func (s *DefaultServerNode) Start() error {
	lis, err := net.Listen("tcp", s.address)
	if err != nil {
		return fmt.Errorf("listen %s: %w", s.address, err)
	}
	s.listener = lis
	go func() {
		if err := s.grpcServer.Serve(lis); err != nil {
			logs.Warnf("gRPC server stopped: %v", err)
		}
	}()
	if s.httpAddr != "" {
		go func() {
			if err := s.serveGateway(s.httpAddr, s.Addr()); err != nil {
				logs.Warnf("gRPC-Gateway stopped: %v", err)
			}
		}()
	}
	return nil
}

// Shutdown stops the gRPC server gracefully.
func (s *DefaultServerNode) Shutdown() error {
	s.grpcServer.GracefulStop()
	return nil
}
