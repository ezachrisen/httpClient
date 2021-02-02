package httpClient

// Convenience wrapper for calling an HTTP service and recording metrics via
// OpenCensus on Google Cloud

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"contrib.go.opencensus.io/exporter/stackdriver/propagation"
	"go.opencensus.io/plugin/ochttp"
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

var (
	// OpenCensus metric definition for outbount latency
	outboundHTTPLatency = stats.Int64("http_outbound_latency", "Latency of the external HTTP API", stats.UnitMilliseconds)

	// OpenCensus metric definition for outbound request count
	outboundHTTPRequests = stats.Int64("http_outbound_count", "Request count to the external HTTP API", stats.UnitDimensionless)

	// The recorded metrics will received the tags defined here

	// MethodTag is the HTTP method: GET, POST, etc.
	// Derived from the HTTP request.
	MethodTag = tag.MustNewKey("http_method")

	// StatusTagCode is the HTTP response status code (404)
	// Derived from the HTTP response.
	StatusTag = tag.MustNewKey("http_status_code")

	// StatusClassTag is the HTTP response status class (2xx, 3xx, etc.)
	// Derived from the HTTP response.
	StatusClassTag = tag.MustNewKey("http_status_class")

	// APINameTag is name of the API called.
	// You supply this name when calling HTTPDo. Should be a human-friendly name, such as
	// /v1/books/search
	APINameTag = tag.MustNewKey("api_name")

	// VersionTag is the version of the application.
	// May indicate the application build or the runtime config.
	// For Cloud Run, it should be the revision name.
	VersionTag = tag.MustNewKey("version_name")
)

func init() {
	registerLatencyMetric(outboundHTTPLatency, []tag.Key{MethodTag, APINameTag, StatusTag, StatusClassTag, VersionTag})
	registerCounterMetric(outboundHTTPRequests, []tag.Key{MethodTag, APINameTag, StatusTag, StatusClassTag, VersionTag})
}

// Do calls the http.Client.Do method with the provided request and returns the response.
// Do sets a timeout on the client call and propagates the Google Cloud Platform trace header.
// If you don't need a timeout (not recommended), set a very long duration.
// The latency and response are sent to OpenCencus metrics.
// Separate errors are returned for failures in the http.Client.Do call, or the call to record metrics.
// httpError can be nil and metricError can be populated (if the HTTP call succeeded, but we couldn't record metrics)
// Similarly, httpError can be populated, but metricError can be nil (if HTTP call failed, but we recorded it in metrics).
func Do(req *http.Request, apiName string, versionName string, timeout time.Duration) (response *http.Response, httpError error, metricError error) {

	start := time.Now()
	client := &http.Client{
		Timeout: timeout,
		Transport: &ochttp.Transport{
			Propagation: &propagation.HTTPFormat{},
		},
	}

	response, httpError = client.Do(req)
	timeTaken := time.Since(start)

	metricError = recordHTTPMetrics(req.Context(), req.Method, apiName, versionName, timeTaken, response)

	return response, httpError, metricError
}

// recordHTTPMetrics records latency and counter metrics to OpenCensus
func recordHTTPMetrics(ctx context.Context, method string, apiName string, versionName string, latency time.Duration, resp *http.Response) error {

	var class string
	var code int

	if resp != nil {
		code = resp.StatusCode
	} else {
		code = 500
	}

	if code >= 100 && code <= 199 {
		class = "1xx"
	} else if code >= 200 && code <= 299 {
		class = "2xx"
	} else if code >= 300 && code <= 399 {
		class = "3xx"
	} else if code >= 400 && code <= 499 {
		class = "4xx"
	} else if code >= 500 && code <= 599 {
		class = "5xx"
	} else {
		class = "UNKNOWN"
	}

	err := stats.RecordWithTags(
		ctx,
		[]tag.Mutator{
			tag.Insert(MethodTag, method),
			tag.Insert(APINameTag, apiName),
			tag.Insert(StatusTag, strconv.Itoa(code)),
			tag.Insert(StatusClassTag, class),
			tag.Insert(VersionTag, versionName),
		},
		outboundHTTPLatency.M(latency.Milliseconds()),
		outboundHTTPRequests.M(1))

	return err

}

// registerLatencyMetric is a helper function to register a stats.Measure with OpenCensus
// This must happen before you start recording metrics.
// This function registers a latency-type metric, that measures execution time
func registerLatencyMetric(m stats.Measure, tags []tag.Key) error {
	v := &view.View{
		Measure:     m,
		Name:        m.Name(),
		TagKeys:     tags,
		Description: m.Description(),
		Aggregation: view.Distribution(0, 100, 200, 400, 1000, 2000, 4000),
	}

	if err := view.Register(v); err != nil {
		return err
	}
	return nil
}

// registerCounterMetric is a helper function to register a stats.Measure with OpenCensus
// This must happen before you start recording metrics.
// This function registers a counter metric used to count the occurences of things.
func registerCounterMetric(m stats.Measure, tags []tag.Key) error {
	v := &view.View{
		Measure:     m,
		Name:        m.Name(),
		TagKeys:     tags,
		Description: m.Description(),
		Aggregation: view.Count(),
	}

	if err := view.Register(v); err != nil {
		return err
	}
	return nil
}
