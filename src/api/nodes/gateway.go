package nodes

import (
	"context"
	"net/http"

	"github.com/danmuck/dps_files/src/api/pb"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// serveGateway starts an HTTP/JSON reverse proxy forwarding to the gRPC server
// at grpcAddr. Blocks until the HTTP server stops.
func (s *DefaultServerNode) serveGateway(httpAddr, grpcAddr string) error {
	ctx := context.Background()
	mux := runtime.NewServeMux()
	opts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	if err := pb.RegisterDPSFilesHandlerFromEndpoint(ctx, mux, grpcAddr, opts); err != nil {
		return err
	}
	return http.ListenAndServe(httpAddr, mux)
}
