// Copyright The OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package otlp_test

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"google.golang.org/grpc"
	"google.golang.org/grpc/encoding/gzip"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp"
	commonpb "go.opentelemetry.io/otel/exporters/otlp/internal/opentelemetry-proto-gen/common/v1"
	"go.opentelemetry.io/otel/label"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/number"
	exportmetric "go.opentelemetry.io/otel/sdk/export/metric"
	exporttrace "go.opentelemetry.io/otel/sdk/export/trace"
	"go.opentelemetry.io/otel/sdk/instrumentation"
	"go.opentelemetry.io/otel/sdk/metric/aggregator/sum"
	"go.opentelemetry.io/otel/sdk/metric/controller/push"
	processor "go.opentelemetry.io/otel/sdk/metric/processor/basic"
	"go.opentelemetry.io/otel/sdk/metric/selector/simple"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

func TestNewExporter_endToEnd(t *testing.T) {
	tests := []struct {
		name           string
		additionalOpts []otlp.GRPCConnectionOption
	}{
		{
			name: "StandardExporter",
		},
		{
			name: "WithCompressor",
			additionalOpts: []otlp.GRPCConnectionOption{
				otlp.WithCompressor(gzip.Name),
			},
		},
		{
			name: "WithGRPCServiceConfig",
			additionalOpts: []otlp.GRPCConnectionOption{
				otlp.WithGRPCServiceConfig("{}"),
			},
		},
		{
			name: "WithGRPCDialOptions",
			additionalOpts: []otlp.GRPCConnectionOption{
				otlp.WithGRPCDialOption(grpc.WithBlock()),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			newExporterEndToEndTest(t, test.additionalOpts)
		})
	}
}

func newGRPCExporter(t *testing.T, ctx context.Context, address string, additionalOpts ...otlp.GRPCConnectionOption) *otlp.Exporter {
	opts := []otlp.GRPCConnectionOption{
		otlp.WithInsecure(),
		otlp.WithAddress(address),
		otlp.WithReconnectionPeriod(50 * time.Millisecond),
	}

	opts = append(opts, additionalOpts...)
	driver := otlp.NewGRPCDriver(opts...)
	exp, err := otlp.NewExporter(ctx, driver)
	if err != nil {
		t.Fatalf("failed to create a new collector exporter: %v", err)
	}
	return exp
}

func runEndToEndTest(t *testing.T, ctx context.Context, exp *otlp.Exporter, mcTraces, mcMetrics *mockCol) {
	pOpts := []sdktrace.TracerProviderOption{
		sdktrace.WithConfig(sdktrace.Config{DefaultSampler: sdktrace.AlwaysSample()}),
		sdktrace.WithBatcher(
			exp,
			// add following two options to ensure flush
			sdktrace.WithBatchTimeout(5),
			sdktrace.WithMaxExportBatchSize(10),
		),
	}
	tp1 := sdktrace.NewTracerProvider(append(pOpts,
		sdktrace.WithResource(resource.NewWithAttributes(
			label.String("rk1", "rv11)"),
			label.Int64("rk2", 5),
		)))...)

	tp2 := sdktrace.NewTracerProvider(append(pOpts,
		sdktrace.WithResource(resource.NewWithAttributes(
			label.String("rk1", "rv12)"),
			label.Float32("rk3", 6.5),
		)))...)

	tr1 := tp1.Tracer("test-tracer1")
	tr2 := tp2.Tracer("test-tracer2")
	// Now create few spans
	m := 4
	for i := 0; i < m; i++ {
		_, span := tr1.Start(ctx, "AlwaysSample")
		span.SetAttributes(label.Int64("i", int64(i)))
		span.End()

		_, span = tr2.Start(ctx, "AlwaysSample")
		span.SetAttributes(label.Int64("i", int64(i)))
		span.End()
	}

	selector := simple.NewWithInexpensiveDistribution()
	processor := processor.New(selector, exportmetric.StatelessExportKindSelector())
	pusher := push.New(processor, exp)
	pusher.Start()

	meter := pusher.MeterProvider().Meter("test-meter")
	labels := []label.KeyValue{label.Bool("test", true)}

	type data struct {
		iKind metric.InstrumentKind
		nKind number.Kind
		val   int64
	}
	instruments := map[string]data{
		"test-int64-counter":         {metric.CounterInstrumentKind, number.Int64Kind, 1},
		"test-float64-counter":       {metric.CounterInstrumentKind, number.Float64Kind, 1},
		"test-int64-valuerecorder":   {metric.ValueRecorderInstrumentKind, number.Int64Kind, 2},
		"test-float64-valuerecorder": {metric.ValueRecorderInstrumentKind, number.Float64Kind, 2},
		"test-int64-valueobserver":   {metric.ValueObserverInstrumentKind, number.Int64Kind, 3},
		"test-float64-valueobserver": {metric.ValueObserverInstrumentKind, number.Float64Kind, 3},
	}
	for name, data := range instruments {
		data := data
		switch data.iKind {
		case metric.CounterInstrumentKind:
			switch data.nKind {
			case number.Int64Kind:
				metric.Must(meter).NewInt64Counter(name).Add(ctx, data.val, labels...)
			case number.Float64Kind:
				metric.Must(meter).NewFloat64Counter(name).Add(ctx, float64(data.val), labels...)
			default:
				assert.Failf(t, "unsupported number testing kind", data.nKind.String())
			}
		case metric.ValueRecorderInstrumentKind:
			switch data.nKind {
			case number.Int64Kind:
				metric.Must(meter).NewInt64ValueRecorder(name).Record(ctx, data.val, labels...)
			case number.Float64Kind:
				metric.Must(meter).NewFloat64ValueRecorder(name).Record(ctx, float64(data.val), labels...)
			default:
				assert.Failf(t, "unsupported number testing kind", data.nKind.String())
			}
		case metric.ValueObserverInstrumentKind:
			switch data.nKind {
			case number.Int64Kind:
				metric.Must(meter).NewInt64ValueObserver(name,
					func(_ context.Context, result metric.Int64ObserverResult) {
						result.Observe(data.val, labels...)
					},
				)
			case number.Float64Kind:
				callback := func(v float64) metric.Float64ObserverFunc {
					return metric.Float64ObserverFunc(func(_ context.Context, result metric.Float64ObserverResult) { result.Observe(v, labels...) })
				}(float64(data.val))
				metric.Must(meter).NewFloat64ValueObserver(name, callback)
			default:
				assert.Failf(t, "unsupported number testing kind", data.nKind.String())
			}
		default:
			assert.Failf(t, "unsupported metrics testing kind", data.iKind.String())
		}
	}

	// Flush and close.
	pusher.Stop()
	func() {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		if err := tp1.Shutdown(ctx); err != nil {
			t.Fatalf("failed to shut down a tracer provider 1: %v", err)
		}
		if err := tp2.Shutdown(ctx); err != nil {
			t.Fatalf("failed to shut down a tracer provider 2: %v", err)
		}
	}()

	// Wait >2 cycles.
	<-time.After(40 * time.Millisecond)

	// Now shutdown the exporter
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := exp.Shutdown(ctx); err != nil {
		t.Fatalf("failed to stop the exporter: %v", err)
	}

	// Shutdown the collector too so that we can begin
	// verification checks of expected data back.
	_ = mcTraces.stop()
	_ = mcMetrics.stop()

	// Now verify that we only got two resources
	rss := mcTraces.getResourceSpans()
	if got, want := len(rss), 2; got != want {
		t.Fatalf("resource span count: got %d, want %d\n", got, want)
	}

	// Now verify spans and attributes for each resource span.
	for _, rs := range rss {
		if len(rs.InstrumentationLibrarySpans) == 0 {
			t.Fatalf("zero Instrumentation Library Spans")
		}
		if got, want := len(rs.InstrumentationLibrarySpans[0].Spans), m; got != want {
			t.Fatalf("span counts: got %d, want %d", got, want)
		}
		attrMap := map[int64]bool{}
		for _, s := range rs.InstrumentationLibrarySpans[0].Spans {
			if gotName, want := s.Name, "AlwaysSample"; gotName != want {
				t.Fatalf("span name: got %s, want %s", gotName, want)
			}
			attrMap[s.Attributes[0].Value.Value.(*commonpb.AnyValue_IntValue).IntValue] = true
		}
		if got, want := len(attrMap), m; got != want {
			t.Fatalf("span attribute unique values: got %d  want %d", got, want)
		}
		for i := 0; i < m; i++ {
			_, ok := attrMap[int64(i)]
			if !ok {
				t.Fatalf("span with attribute %d missing", i)
			}
		}
	}

	metrics := mcMetrics.getMetrics()
	assert.Len(t, metrics, len(instruments), "not enough metrics exported")
	seen := make(map[string]struct{}, len(instruments))
	for _, m := range metrics {
		data, ok := instruments[m.Name]
		if !ok {
			assert.Failf(t, "unknown metrics", m.Name)
			continue
		}
		seen[m.Name] = struct{}{}

		switch data.iKind {
		case metric.CounterInstrumentKind:
			switch data.nKind {
			case number.Int64Kind:
				if dp := m.GetIntSum().DataPoints; assert.Len(t, dp, 1) {
					assert.Equal(t, data.val, dp[0].Value, "invalid value for %q", m.Name)
				}
			case number.Float64Kind:
				if dp := m.GetDoubleSum().DataPoints; assert.Len(t, dp, 1) {
					assert.Equal(t, float64(data.val), dp[0].Value, "invalid value for %q", m.Name)
				}
			default:
				assert.Failf(t, "invalid number kind", data.nKind.String())
			}
		case metric.ValueObserverInstrumentKind:
			switch data.nKind {
			case number.Int64Kind:
				if dp := m.GetIntGauge().DataPoints; assert.Len(t, dp, 1) {
					assert.Equal(t, data.val, dp[0].Value, "invalid value for %q", m.Name)
				}
			case number.Float64Kind:
				if dp := m.GetDoubleGauge().DataPoints; assert.Len(t, dp, 1) {
					assert.Equal(t, float64(data.val), dp[0].Value, "invalid value for %q", m.Name)
				}
			default:
				assert.Failf(t, "invalid number kind", data.nKind.String())
			}
		case metric.ValueRecorderInstrumentKind:
			switch data.nKind {
			case number.Int64Kind:
				assert.NotNil(t, m.GetIntHistogram())
				if dp := m.GetIntHistogram().DataPoints; assert.Len(t, dp, 1) {
					count := dp[0].Count
					assert.Equal(t, uint64(1), count, "invalid count for %q", m.Name)
					assert.Equal(t, int64(data.val*int64(count)), dp[0].Sum, "invalid sum for %q (value %d)", m.Name, data.val)
				}
			case number.Float64Kind:
				assert.NotNil(t, m.GetDoubleHistogram())
				if dp := m.GetDoubleHistogram().DataPoints; assert.Len(t, dp, 1) {
					count := dp[0].Count
					assert.Equal(t, uint64(1), count, "invalid count for %q", m.Name)
					assert.Equal(t, float64(data.val*int64(count)), dp[0].Sum, "invalid sum for %q (value %d)", m.Name, data.val)
				}
			default:
				assert.Failf(t, "invalid number kind", data.nKind.String())
			}
		default:
			assert.Failf(t, "invalid metrics kind", data.iKind.String())
		}
	}

	for i := range instruments {
		if _, ok := seen[i]; !ok {
			assert.Fail(t, fmt.Sprintf("no metric(s) exported for %q", i))
		}
	}
}

func newExporterEndToEndTest(t *testing.T, additionalOpts []otlp.GRPCConnectionOption) {
	mc := runMockColAtAddr(t, "localhost:56561")

	defer func() {
		_ = mc.stop()
	}()

	<-time.After(5 * time.Millisecond)

	ctx := context.Background()
	exp := newGRPCExporter(t, ctx, mc.address, additionalOpts...)
	defer func() {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		if err := exp.Shutdown(ctx); err != nil {
			panic(err)
		}
	}()

	runEndToEndTest(t, ctx, exp, mc, mc)
}

func TestNewExporter_invokeStartThenStopManyTimes(t *testing.T) {
	mc := runMockCol(t)
	defer func() {
		_ = mc.stop()
	}()

	ctx := context.Background()
	exp := newGRPCExporter(t, ctx, mc.address)
	defer func() {
		if err := exp.Shutdown(ctx); err != nil {
			panic(err)
		}
	}()

	// Invoke Start numerous times, should return errAlreadyStarted
	for i := 0; i < 10; i++ {
		if err := exp.Start(ctx); err == nil || !strings.Contains(err.Error(), "already started") {
			t.Fatalf("#%d unexpected Start error: %v", i, err)
		}
	}

	if err := exp.Shutdown(ctx); err != nil {
		t.Fatalf("failed to Shutdown the exporter: %v", err)
	}
	// Invoke Shutdown numerous times
	for i := 0; i < 10; i++ {
		if err := exp.Shutdown(ctx); err != nil {
			t.Fatalf(`#%d got error (%v) expected none`, i, err)
		}
	}
}

func TestNewExporter_collectorConnectionDiesThenReconnects(t *testing.T) {
	mc := runMockCol(t)

	reconnectionPeriod := 20 * time.Millisecond
	ctx := context.Background()
	exp := newGRPCExporter(t, ctx, mc.address,
		otlp.WithReconnectionPeriod(reconnectionPeriod))
	defer func() {
		_ = exp.Shutdown(ctx)
	}()

	// We'll now stop the collector right away to simulate a connection
	// dying in the midst of communication or even not existing before.
	_ = mc.stop()

	// In the test below, we'll stop the collector many times,
	// while exporting traces and test to ensure that we can
	// reconnect.
	for j := 0; j < 3; j++ {

		// No endpoint up.
		require.Error(
			t,
			exp.ExportSpans(ctx, []*exporttrace.SpanSnapshot{{Name: "in the midst"}}),
			"transport: Error while dialing dial tcp %s: connect: connection refused",
			mc.address,
		)

		// Now resurrect the collector by making a new one but reusing the
		// old address, and the collector should reconnect automatically.
		nmc := runMockColAtAddr(t, mc.address)

		// Give the exporter sometime to reconnect
		<-time.After(reconnectionPeriod * 4)

		n := 10
		for i := 0; i < n; i++ {
			require.NoError(t, exp.ExportSpans(ctx, []*exporttrace.SpanSnapshot{{Name: "Resurrected"}}))
		}

		nmaSpans := nmc.getSpans()
		// Expecting 10 SpanSnapshots that were sampled, given that
		if g, w := len(nmaSpans), n; g != w {
			t.Fatalf("Round #%d: Connected collector: spans: got %d want %d", j, g, w)
		}

		dSpans := mc.getSpans()
		// Expecting 0 spans to have been received by the original but now dead collector
		if g, w := len(dSpans), 0; g != w {
			t.Fatalf("Round #%d: Disconnected collector: spans: got %d want %d", j, g, w)
		}
		_ = nmc.stop()
	}
}

// This test takes a long time to run: to skip it, run tests using: -short
func TestNewExporter_collectorOnBadConnection(t *testing.T) {
	if testing.Short() {
		t.Skipf("Skipping this long running test")
	}

	ln, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to grab an available port: %v", err)
	}
	// Firstly close the "collector's" channel: optimistically this address won't get reused ASAP
	// However, our goal of closing it is to simulate an unavailable connection
	_ = ln.Close()

	_, collectorPortStr, _ := net.SplitHostPort(ln.Addr().String())

	address := fmt.Sprintf("localhost:%s", collectorPortStr)
	ctx := context.Background()
	exp := newGRPCExporter(t, ctx, address)
	_ = exp.Shutdown(ctx)
}

func TestNewExporter_withAddress(t *testing.T) {
	mc := runMockCol(t)
	defer func() {
		_ = mc.stop()
	}()

	ctx := context.Background()
	exp := newGRPCExporter(t, ctx, mc.address)
	_ = exp.Shutdown(ctx)
}

func TestNewExporter_withHeaders(t *testing.T) {
	mc := runMockCol(t)
	defer func() {
		_ = mc.stop()
	}()

	ctx := context.Background()
	exp := newGRPCExporter(t, ctx, mc.address,
		otlp.WithHeaders(map[string]string{"header1": "value1"}))
	require.NoError(t, exp.ExportSpans(ctx, []*exporttrace.SpanSnapshot{{Name: "in the midst"}}))

	defer func() {
		_ = exp.Shutdown(ctx)
	}()

	headers := mc.getHeaders()
	require.Len(t, headers.Get("header1"), 1)
	assert.Equal(t, "value1", headers.Get("header1")[0])
}

func TestNewExporter_withMultipleAttributeTypes(t *testing.T) {
	mc := runMockCol(t)

	defer func() {
		_ = mc.stop()
	}()

	<-time.After(5 * time.Millisecond)

	ctx := context.Background()
	exp := newGRPCExporter(t, ctx, mc.address)

	defer func() {
		_ = exp.Shutdown(ctx)
	}()

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithConfig(sdktrace.Config{DefaultSampler: sdktrace.AlwaysSample()}),
		sdktrace.WithBatcher(
			exp,
			// add following two options to ensure flush
			sdktrace.WithBatchTimeout(5),
			sdktrace.WithMaxExportBatchSize(10),
		),
	)
	defer func() { _ = tp.Shutdown(ctx) }()

	tr := tp.Tracer("test-tracer")
	testKvs := []label.KeyValue{
		label.Int("Int", 1),
		label.Int32("Int32", int32(2)),
		label.Int64("Int64", int64(3)),
		label.Float32("Float32", float32(1.11)),
		label.Float64("Float64", 2.22),
		label.Bool("Bool", true),
		label.String("String", "test"),
	}
	_, span := tr.Start(ctx, "AlwaysSample")
	span.SetAttributes(testKvs...)
	span.End()

	// Flush and close.
	func() {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		if err := tp.Shutdown(ctx); err != nil {
			t.Fatalf("failed to shut down a tracer provider: %v", err)
		}
	}()

	// Wait >2 cycles.
	<-time.After(40 * time.Millisecond)

	// Now shutdown the exporter
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := exp.Shutdown(ctx); err != nil {
		t.Fatalf("failed to stop the exporter: %v", err)
	}

	// Shutdown the collector too so that we can begin
	// verification checks of expected data back.
	_ = mc.stop()

	// Now verify that we only got one span
	rss := mc.getSpans()
	if got, want := len(rss), 1; got != want {
		t.Fatalf("resource span count: got %d, want %d\n", got, want)
	}

	expected := []*commonpb.KeyValue{
		{
			Key: "Int",
			Value: &commonpb.AnyValue{
				Value: &commonpb.AnyValue_IntValue{
					IntValue: 1,
				},
			},
		},
		{
			Key: "Int32",
			Value: &commonpb.AnyValue{
				Value: &commonpb.AnyValue_IntValue{
					IntValue: 2,
				},
			},
		},
		{
			Key: "Int64",
			Value: &commonpb.AnyValue{
				Value: &commonpb.AnyValue_IntValue{
					IntValue: 3,
				},
			},
		},
		{
			Key: "Float32",
			Value: &commonpb.AnyValue{
				Value: &commonpb.AnyValue_DoubleValue{
					DoubleValue: 1.11,
				},
			},
		},
		{
			Key: "Float64",
			Value: &commonpb.AnyValue{
				Value: &commonpb.AnyValue_DoubleValue{
					DoubleValue: 2.22,
				},
			},
		},
		{
			Key: "Bool",
			Value: &commonpb.AnyValue{
				Value: &commonpb.AnyValue_BoolValue{
					BoolValue: true,
				},
			},
		},
		{
			Key: "String",
			Value: &commonpb.AnyValue{
				Value: &commonpb.AnyValue_StringValue{
					StringValue: "test",
				},
			},
		},
	}

	// Verify attributes
	if !assert.Len(t, rss[0].Attributes, len(expected)) {
		t.Fatalf("attributes count: got %d, want %d\n", len(rss[0].Attributes), len(expected))
	}
	for i, actual := range rss[0].Attributes {
		if a, ok := actual.Value.Value.(*commonpb.AnyValue_DoubleValue); ok {
			e, ok := expected[i].Value.Value.(*commonpb.AnyValue_DoubleValue)
			if !ok {
				t.Errorf("expected AnyValue_DoubleValue, got %T", expected[i].Value.Value)
				continue
			}
			if !assert.InDelta(t, e.DoubleValue, a.DoubleValue, 0.01) {
				continue
			}
			e.DoubleValue = a.DoubleValue
		}
		assert.Equal(t, expected[i], actual)
	}
}

type discCheckpointSet struct{}

func (discCheckpointSet) ForEach(kindSelector exportmetric.ExportKindSelector, recordFunc func(exportmetric.Record) error) error {
	desc := metric.NewDescriptor(
		"foo",
		metric.CounterInstrumentKind,
		number.Int64Kind,
	)
	res := resource.NewWithAttributes(label.String("a", "b"))
	agg := sum.New(1)
	start := time.Now().Add(-20 * time.Minute)
	end := time.Now()
	labels := label.NewSet()
	rec := exportmetric.NewRecord(&desc, &labels, res, agg[0].Aggregation(), start, end)
	return recordFunc(rec)
}

func (discCheckpointSet) Lock()    {}
func (discCheckpointSet) Unlock()  {}
func (discCheckpointSet) RLock()   {}
func (discCheckpointSet) RUnlock() {}

func discSpanSnapshot() *exporttrace.SpanSnapshot {
	return &exporttrace.SpanSnapshot{
		SpanContext: trace.SpanContext{
			TraceID:    trace.TraceID{2, 3, 4, 5, 6, 7, 8, 9, 2, 3, 4, 5, 6, 7, 8, 9},
			SpanID:     trace.SpanID{3, 4, 5, 6, 7, 8, 9, 0},
			TraceFlags: trace.FlagsSampled,
		},
		ParentSpanID:             trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8},
		SpanKind:                 trace.SpanKindInternal,
		Name:                     "foo",
		StartTime:                time.Now().Add(-20 * time.Minute),
		EndTime:                  time.Now(),
		Attributes:               []label.KeyValue{},
		MessageEvents:            []exporttrace.Event{},
		Links:                    []trace.Link{},
		StatusCode:               codes.Ok,
		StatusMessage:            "",
		HasRemoteParent:          false,
		DroppedAttributeCount:    0,
		DroppedMessageEventCount: 0,
		DroppedLinkCount:         0,
		ChildSpanCount:           0,
		Resource:                 resource.NewWithAttributes(label.String("a", "b")),
		InstrumentationLibrary: instrumentation.Library{
			Name:    "bar",
			Version: "0.0.0",
		},
	}
}

func TestDisconnected(t *testing.T) {
	ctx := context.Background()
	// The address is whatever, we want to be disconnected. But we
	// setting a blocking connection, so dialing to the invalid
	// address actually fails.
	exp := newGRPCExporter(t, ctx, "invalid",
		otlp.WithReconnectionPeriod(time.Hour),
		otlp.WithGRPCDialOption(
			grpc.WithBlock(),
			grpc.FailOnNonTempDialError(true),
		),
	)
	defer func() {
		assert.NoError(t, exp.Shutdown(ctx))
	}()

	assert.Error(t, exp.Export(ctx, discCheckpointSet{}))
	assert.Error(t, exp.ExportSpans(ctx, []*exporttrace.SpanSnapshot{discSpanSnapshot()}))
}

type emptyCheckpointSet struct{}

func (emptyCheckpointSet) ForEach(kindSelector exportmetric.ExportKindSelector, recordFunc func(exportmetric.Record) error) error {
	return nil
}

func (emptyCheckpointSet) Lock()    {}
func (emptyCheckpointSet) Unlock()  {}
func (emptyCheckpointSet) RLock()   {}
func (emptyCheckpointSet) RUnlock() {}

func TestEmptyData(t *testing.T) {
	mc := runMockColAtAddr(t, "localhost:56561")

	defer func() {
		_ = mc.stop()
	}()

	<-time.After(5 * time.Millisecond)

	ctx := context.Background()
	exp := newGRPCExporter(t, ctx, mc.address)
	defer func() {
		assert.NoError(t, exp.Shutdown(ctx))
	}()

	assert.NoError(t, exp.ExportSpans(ctx, nil))
	assert.NoError(t, exp.Export(ctx, emptyCheckpointSet{}))
}

type failCheckpointSet struct{}

func (failCheckpointSet) ForEach(kindSelector exportmetric.ExportKindSelector, recordFunc func(exportmetric.Record) error) error {
	return fmt.Errorf("fail")
}

func (failCheckpointSet) Lock()    {}
func (failCheckpointSet) Unlock()  {}
func (failCheckpointSet) RLock()   {}
func (failCheckpointSet) RUnlock() {}

func TestFailedMetricTransform(t *testing.T) {
	mc := runMockColAtAddr(t, "localhost:56561")

	defer func() {
		_ = mc.stop()
	}()

	<-time.After(5 * time.Millisecond)

	ctx := context.Background()
	exp := newGRPCExporter(t, ctx, mc.address)
	defer func() {
		assert.NoError(t, exp.Shutdown(ctx))
	}()

	assert.Error(t, exp.Export(ctx, failCheckpointSet{}))
}

func TestMultiConnectionDriver(t *testing.T) {
	mcTraces := runMockCol(t)
	mcMetrics := runMockCol(t)

	defer func() {
		_ = mcTraces.stop()
		_ = mcMetrics.stop()
	}()

	<-time.After(5 * time.Millisecond)

	commonOpts := []otlp.GRPCConnectionOption{
		otlp.WithInsecure(),
		otlp.WithReconnectionPeriod(50 * time.Millisecond),
		otlp.WithGRPCDialOption(grpc.WithBlock()),
	}
	optsTraces := append([]otlp.GRPCConnectionOption{
		otlp.WithAddress(mcTraces.address),
	}, commonOpts...)
	optsMetrics := append([]otlp.GRPCConnectionOption{
		otlp.WithAddress(mcMetrics.address),
	}, commonOpts...)

	tracesDriver := otlp.NewGRPCDriver(optsTraces...)
	metricsDriver := otlp.NewGRPCDriver(optsMetrics...)
	splitCfg := otlp.SplitConfig{
		ForMetrics: metricsDriver,
		ForTraces:  tracesDriver,
	}
	driver := otlp.NewSplitDriver(splitCfg)
	ctx := context.Background()
	exp, err := otlp.NewExporter(ctx, driver)
	if err != nil {
		t.Fatalf("failed to create a new collector exporter: %v", err)
	}
	defer func() {
		assert.NoError(t, exp.Shutdown(ctx))
	}()
	runEndToEndTest(t, ctx, exp, mcTraces, mcMetrics)
}