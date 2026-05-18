package server

import (
	"bytes"
	"context"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/kopia/kopia/internal/grpcapi"
	"github.com/kopia/kopia/repo/logging"
)

type fakeStream struct {
	grpc.ServerStream
	ctx  context.Context
	sent []any
}

func (f *fakeStream) Context() context.Context { return f.ctx }
func (f *fakeStream) SendMsg(m any) error       { f.sent = append(f.sent, m); return nil }
func (f *fakeStream) RecvMsg(any) error          { return nil }
func (f *fakeStream) SetHeader(metadata.MD) error { return nil }
func (f *fakeStream) SendHeader(metadata.MD) error { return nil }
func (f *fakeStream) SetTrailer(metadata.MD)       {}

func TestErrorLoggingStream_SendMsg(t *testing.T) {
	tests := []struct {
		name       string
		msg        any
		wantLogged string
	}{
		{
			name: "logs UNKNOWN_ERROR response",
			msg: &grpcapi.SessionResponse{
				Response: &grpcapi.SessionResponse_Error{
					Error: &grpcapi.ErrorResponse{
						Code:    grpcapi.ErrorResponse_UNKNOWN_ERROR,
						Message: "mkdir /data/base/s/428: no space left on device",
					},
				},
			},
			wantLogged: "no space left on device",
		},
		{
			name: "ignores non-error response",
			msg: &grpcapi.SessionResponse{
				Response: &grpcapi.SessionResponse_GetContentInfo{},
			},
			wantLogged: "",
		},
		{
			name:       "ignores non-SessionResponse message",
			msg:        &grpcapi.SessionRequest{},
			wantLogged: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			ctx := logging.WithLogger(context.Background(), logging.ToWriter(&buf))

			fake := &fakeStream{ctx: ctx}
			stream := &errorLoggingStream{ServerStream: fake}

			if err := stream.SendMsg(tc.msg); err != nil {
				t.Fatalf("SendMsg returned unexpected error: %v", err)
			}

			if len(fake.sent) != 1 {
				t.Fatalf("expected message to be forwarded, got %d sends", len(fake.sent))
			}

			logged := buf.String()
			if tc.wantLogged == "" && logged != "" {
				t.Errorf("expected no log output, got: %s", logged)
			}

			if tc.wantLogged != "" && !bytes.Contains(buf.Bytes(), []byte(tc.wantLogged)) {
				t.Errorf("expected log to contain %q, got: %s", tc.wantLogged, logged)
			}
		})
	}
}

func TestSessionErrorLogInterceptor(t *testing.T) {
	var buf bytes.Buffer
	ctx := logging.WithLogger(context.Background(), logging.ToWriter(&buf))

	fake := &fakeStream{ctx: ctx}
	interceptor := sessionErrorLogInterceptor()

	var handlerStream grpc.ServerStream

	err := interceptor(nil, fake, nil, func(_ any, ss grpc.ServerStream) error {
		handlerStream = ss
		return ss.SendMsg(&grpcapi.SessionResponse{
			Response: &grpcapi.SessionResponse_Error{
				Error: &grpcapi.ErrorResponse{
					Code:    grpcapi.ErrorResponse_UNKNOWN_ERROR,
					Message: "disk full",
				},
			},
		})
	})

	if err != nil {
		t.Fatalf("interceptor returned unexpected error: %v", err)
	}

	if _, ok := handlerStream.(*errorLoggingStream); !ok {
		t.Errorf("expected handler to receive *errorLoggingStream, got %T", handlerStream)
	}

	if !bytes.Contains(buf.Bytes(), []byte("disk full")) {
		t.Errorf("expected log to contain 'disk full', got: %s", buf.String())
	}
}
