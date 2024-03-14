package server

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"

	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"github.com/davecgh/go-spew/spew"
	"github.com/nomoresecretz/ghoeq-common/decoder"
	pb "github.com/nomoresecretz/ghoeq-common/proto/ghoeq"
)

var opDefs = ""

func TestServerFiles(t *testing.T) {
	tests := map[string]struct {
		interfaceList []string
		source        string
		want          any
		wantErr       bool
		wantClient    *pb.Client
	}{
		/*
			"Basic-1": {
				[]string{"file://test1.pcap"},
				"eth0",
				nil,
				false,
				nil,
			},
		*/
	}

	d := decoder.NewDecoder()
	if err := d.LoadMap(opDefs); err != nil {
		t.Fatalf("failed to create decoder: %s", err)
	}

	for i, test := range tests {
		testName := i

		// Each path turns into a test: the test name is the filename without the
		// extension.
		t.Run(testName, func(t *testing.T) {
			var gameClient *pb.Client
			ctx := context.Background()

			s, err := New(ctx, test.interfaceList)
			if err != nil {
				t.Fatalf("failed to make server: %s", err)
			}

			eg, nctx := errgroup.WithContext(ctx)

			client, cf := testClient(t, nctx, s, eg)
			defer cf()

			eg.Go(func() error {
				return s.Run(nctx, d)
			})
			// Start test here

			_, err = client.ModifySession(ctx, &pb.ModifySessionRequest{
				Mods: []*pb.ModifyRequest{{
					State:  pb.State_STATE_START,
					Source: test.source,
				}},
			})
			if err != nil {
				t.Fatalf("failed to start session: %v", err)
			}

			cClient, err := client.AttachClient(ctx, &pb.AttachClientRequest{})
			if err != nil {
				t.Fatalf("failed to start session: %v", err)
			}

			eg.Go(func() error {
				for {
					clientRes, err := cClient.Recv()
					if errors.Is(err, io.EOF) {
						break
					}

					if err != nil {
						t.Fatalf("client follow err: %v", err)
					}

					if c := clientRes.GetClient(); c != nil && gameClient == nil {
						gameClient = c
					}

					for _, s := range clientRes.GetStreams() {
						eg.Go(func() error {
							return testStream(t, nctx, client, s)
						})
					}
				}

				return nil
			})

			// End test here
			cf()

			err = eg.Wait()
			if (err == nil) != test.wantErr {
				t.Errorf("Err got %v when wantErr %t", err, test.wantErr)
			}

			// TODO: Final test comparison
			//if diff := pretty.Diff(want, got); len(diff) != 0 {
			//	t.Errorf("test diff: %s", diff)
			//}
		})
	}
}

func testStream(t *testing.T, ctx context.Context, client pb.BackendServerClient, s *pb.Stream) error {
	t.Helper()

	sClient, err := client.AttachStreamStruct(ctx, &pb.AttachStreamRequest{
		Id: s.GetId(),
	})
	if err != nil {
		return err
	}

	for {
		p, err := sClient.Recv()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return err
		}

		spew.Dump(p)
		// TODO: test packets somehow, probably just a big array per stream
	}

	return nil
}

func testClient(t *testing.T, ctx context.Context, server pb.BackendServerServer, eg *errgroup.Group) (pb.BackendServerClient, func()) {
	t.Helper()

	buffer := 101024 * 1024
	lis := bufconn.Listen(buffer)

	baseServer := grpc.NewServer()
	pb.RegisterBackendServerServer(baseServer, server)
	eg.Go(func() error {
		return baseServer.Serve(lis)
	})

	conn, err := grpc.DialContext(ctx, "",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
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
