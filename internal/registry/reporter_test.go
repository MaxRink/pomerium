package registry

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	oteltrace "go.opentelemetry.io/otel/trace"
	googlegrpc "google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/pomerium/pomerium/config"
	pb "github.com/pomerium/pomerium/pkg/grpc/registry"
)

func TestMetricsURL(t *testing.T) {
	for opt, expect := range map[*config.Options]string{
		{MetricsAddr: "my.host:9090"}: "http://my.host:9090/metrics",
		{MetricsAddr: "my.host:9090", MetricsBasicAuth: "bXl1c2VyOm15cGFzc3dvcmQ="}: "http://myuser:mypassword@my.host:9090/metrics",
		{MetricsAddr: "my.host:9090", MetricsCertificate: "CERT"}:                   "https://my.host:9090/metrics",
		{MetricsAddr: "my.host:9090", MetricsCertificateFile: "CERT"}:               "https://my.host:9090/metrics",
	} {
		u, err := metricsURL(*opt)
		if assert.NoError(t, err, opt) {
			assert.Equal(t, expect, u.String())
		}
	}

	for _, opt := range []config.Options{
		{MetricsAddr: "my.host:"},
		{MetricsAddr: "my.host:9090", MetricsBasicAuth: "SMTH"},
		{MetricsAddr: "my.host"},
	} {
		_, err := metricsURL(opt)
		assert.Error(t, err, opt)
	}
}

type traceRecordingRegistryClient struct {
	pb.RegistryClient
	tracer   oteltrace.Tracer
	cancel   context.CancelFunc
	traceIDs []oteltrace.TraceID
}

func (c *traceRecordingRegistryClient) Report(ctx context.Context, _ *pb.RegisterRequest, _ ...googlegrpc.CallOption) (*pb.RegisterResponse, error) {
	_, span := c.tracer.Start(ctx, "Report")
	c.traceIDs = append(c.traceIDs, span.SpanContext().TraceID())
	span.End()
	if len(c.traceIDs) == 2 {
		c.cancel()
	}
	return &pb.RegisterResponse{CallBackAfter: durationpb.New(time.Millisecond)}, nil
}

func TestRunReporterDoesNotInheritParentSpan(t *testing.T) {
	tp := sdktrace.NewTracerProvider()
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("test")

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ctx, parent := tracer.Start(ctx, "UpdateRecords")
	parent.End()

	client := &traceRecordingRegistryClient{tracer: tracer, cancel: cancel}
	runReporter(ctx, client, nil)

	require.Len(t, client.traceIDs, 2)
	assert.NotEqual(t, parent.SpanContext().TraceID(), client.traceIDs[0])
	assert.NotEqual(t, parent.SpanContext().TraceID(), client.traceIDs[1])
	assert.NotEqual(t, client.traceIDs[0], client.traceIDs[1])
}
