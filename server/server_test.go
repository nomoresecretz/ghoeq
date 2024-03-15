package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"testing"

	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"github.com/google/go-cmp/cmp"
	"github.com/nomoresecretz/ghoeq-common/decoder"
	pb "github.com/nomoresecretz/ghoeq-common/proto/ghoeq"
)

func TestServerFiles(t *testing.T) {
	tests := map[string]struct {
		addInterface bool
		source       string
		want         any
		wantErr      bool
		wantClient   *pb.Client
	}{}

	opDefs := os.Getenv("OPDEFS")
	d := decoder.NewDecoder()
	if err := d.LoadMap(opDefs); opDefs != "" && err != nil {
		t.Fatalf("failed to create decoder: %s", err)
	}

	for i, test := range tests {
		testName := i

		// Each path turns into a test: the test name is the filename without the
		// extension.
		t.Run(testName, func(t *testing.T) {
			ctx := context.Background()
			egS, sCtx := errgroup.WithContext(ctx)
			egC, cCtx := errgroup.WithContext(ctx)

			interfaceList := []string{}
			if test.addInterface {
				interfaceList = append(interfaceList, test.source)
			}

			var gameClient *pb.Client

			s, err := New(ctx, interfaceList)
			if err != nil {
				t.Fatalf("failed to make server: %s", err)
			}

			client, cf := testClient(t, sCtx, s, egS)
			defer cf()

			egS.Go(func() error {
				return s.Run(sCtx, d, true)
			})

			// Start test here

			rpl, err := client.ModifySession(ctx, &pb.ModifySessionRequest{
				Mods: []*pb.ModifyRequest{{
					State:  pb.State_STATE_START,
					Source: test.source,
				}},
			})
			if err != nil {
				t.Fatalf("failed to start session: %v", err)
			}

			sesId := rpl.GetResponses()[0].Id

			cClient, err := client.AttachClient(ctx, &pb.AttachClientRequest{})
			if err != nil {
				t.Fatalf("failed to start session: %v", err)
			}

			packets := make(map[string]*[]any)
			packetMu := &sync.RWMutex{}

			egC.Go(func() error {
				for {
					clientRes, err := cClient.Recv()
					if errors.Is(err, io.EOF) {
						return nil
					}

					if err != nil {
						t.Fatalf("client follow err: %v", err)
					}

					if c := clientRes.GetClient(); c != nil && gameClient == nil {
						gameClient = c
					}

					for _, s := range clientRes.GetStreams() {
						egC.Go(func() error {
							return testStream(t, cCtx, client, s, packets, sesId, packetMu)
						})
					}
				}
			})

			// end test here

			err = egC.Wait()
			if (err != nil) != test.wantErr {
				t.Errorf("Err got %v when wantErr %t", err, test.wantErr)
			}

			cf()

			err = egS.Wait()
			if (err != nil) != test.wantErr {
				t.Errorf("Err got %v when wantErr %t", err, test.wantErr)
			}

			if diff := cmp.Diff(test.want, packets); diff != "" {
				t.Errorf("test diff: %s", diff)
			}
		})
	}
}

func testStream(t *testing.T, ctx context.Context, client pb.BackendServerClient, s *pb.Stream, out map[string]*[]any, sesId string, packetMu *sync.RWMutex) error {
	// t.Helper()

	sClient, err := client.AttachStreamRaw(ctx, &pb.AttachStreamRequest{
		Id:        s.GetId(),
		SessionId: sesId,
	})
	if err != nil {
		return fmt.Errorf("failed attaching stream: %w", err)
	}

	arr := []any{}

	packetMu.Lock()
	out[s.Id] = &arr
	packetMu.Unlock()

	for {
		p, err := sClient.Recv()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return fmt.Errorf("packet stream err: %w", err)
		}

		arr = append(arr, p)
	}

	return nil
}

func testClient(t *testing.T, ctx context.Context, server pb.BackendServerServer, eg *errgroup.Group) (pb.BackendServerClient, func()) {
	buffer := 101024 * 1024
	lis := bufconn.Listen(buffer)

	baseServer := grpc.NewServer()
	pb.RegisterBackendServerServer(baseServer, server)
	eg.Go(func() error {
		return baseServer.Serve(lis)
	})

	conn, err := grpc.DialContext(ctx, "",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("error connecting to server: %v", err)
	}

	closer := func() {
		err := lis.Close()
		if err != nil {
			t.Fatalf("error closing listener: %v", err)
		}
		baseServer.Stop()
	}

	client := pb.NewBackendServerClient(conn)

	return client, closer
}
