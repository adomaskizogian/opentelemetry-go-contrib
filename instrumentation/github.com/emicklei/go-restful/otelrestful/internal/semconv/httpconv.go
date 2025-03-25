// Code created by gotmpl. DO NOT MODIFY.
// source: internal/shared/semconv/httpconv.go.tmpl

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package semconv // import "go.opentelemetry.io/contrib/instrumentation/github.com/emicklei/go-restful/otelrestful/internal/semconv"

import (
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
	semconvNew "go.opentelemetry.io/otel/semconv/v1.26.0"
)

type RequestTraceAttrsOpts struct {
	// If set, this is used as value for the "http.client_ip" attribute.
	HTTPClientIP string
}

type CurrentHTTPServer struct{}

// RequestTraceAttrs returns trace attributes for an HTTP request received by a
// server.
//
// The server must be the primary server name if it is known. For example this
// would be the ServerName directive
// (https://httpd.apache.org/docs/2.4/mod/core.html#servername) for an Apache
// server, and the server_name directive
// (http://nginx.org/en/docs/http/ngx_http_core_module.html#server_name) for an
// nginx server. More generically, the primary server name would be the host
// header value that matches the default virtual host of an HTTP server. It
// should include the host identifier and if a port is used to route to the
// server that port identifier should be included as an appropriate port
// suffix.
//
// If the primary server name is not known, server should be an empty string.
// The req Host will be used to determine the server instead.
func (n CurrentHTTPServer) RequestTraceAttrs(server string, req *http.Request, opts RequestTraceAttrsOpts) []attribute.KeyValue {
	count := 3 // ServerAddress, Method, Scheme

	var host string
	var p int
	if server == "" {
		host, p = SplitHostPort(req.Host)
	} else {
		// Prioritize the primary server name.
		host, p = SplitHostPort(server)
		if p < 0 {
			_, p = SplitHostPort(req.Host)
		}
	}

	hostPort := requiredHTTPPort(req.TLS != nil, p)
	if hostPort > 0 {
		count++
	}

	method, methodOriginal := n.method(req.Method)
	if methodOriginal != (attribute.KeyValue{}) {
		count++
	}

	scheme := n.scheme(req.TLS != nil)

	peer, peerPort := SplitHostPort(req.RemoteAddr)
	if peer != "" {
		// The Go HTTP server sets RemoteAddr to "IP:port", this will not be a
		// file-path that would be interpreted with a sock family.
		count++
		if peerPort > 0 {
			count++
		}
	}

	useragent := req.UserAgent()
	if useragent != "" {
		count++
	}

	// For client IP, use, in order:
	// 1. The value passed in the options
	// 2. The value in the X-Forwarded-For header
	// 3. The peer address
	clientIP := opts.HTTPClientIP
	if clientIP == "" {
		clientIP = serverClientIP(req.Header.Get("X-Forwarded-For"))
		if clientIP == "" {
			clientIP = peer
		}
	}
	if clientIP != "" {
		count++
	}

	if req.URL != nil && req.URL.Path != "" {
		count++
	}

	protoName, protoVersion := netProtocol(req.Proto)
	if protoName != "" && protoName != "http" {
		count++
	}
	if protoVersion != "" {
		count++
	}

	httpRoute := req.Pattern
	if httpRoute != "" {
		count++
	}

	attrs := make([]attribute.KeyValue, 0, count)
	attrs = append(attrs,
		semconvNew.ServerAddress(host),
		method,
		scheme,
	)

	if hostPort > 0 {
		attrs = append(attrs, semconvNew.ServerPort(hostPort))
	}
	if methodOriginal != (attribute.KeyValue{}) {
		attrs = append(attrs, methodOriginal)
	}

	if peer, peerPort := SplitHostPort(req.RemoteAddr); peer != "" {
		// The Go HTTP server sets RemoteAddr to "IP:port", this will not be a
		// file-path that would be interpreted with a sock family.
		attrs = append(attrs, semconvNew.NetworkPeerAddress(peer))
		if peerPort > 0 {
			attrs = append(attrs, semconvNew.NetworkPeerPort(peerPort))
		}
	}

	if useragent := req.UserAgent(); useragent != "" {
		attrs = append(attrs, semconvNew.UserAgentOriginal(useragent))
	}

	if clientIP != "" {
		attrs = append(attrs, semconvNew.ClientAddress(clientIP))
	}

	if req.URL != nil && req.URL.Path != "" {
		attrs = append(attrs, semconvNew.URLPath(req.URL.Path))
	}

	if protoName != "" && protoName != "http" {
		attrs = append(attrs, semconvNew.NetworkProtocolName(protoName))
	}
	if protoVersion != "" {
		attrs = append(attrs, semconvNew.NetworkProtocolVersion(protoVersion))
	}

	if httpRoute != "" {
		attrs = append(attrs, n.Route(httpRoute))
	}

	return attrs
}

func (o CurrentHTTPServer) NetworkTransportAttr(network string) attribute.KeyValue {
	switch network {
	case "tcp", "tcp4", "tcp6":
		return semconvNew.NetworkTransportTCP
	case "udp", "udp4", "udp6":
		return semconvNew.NetworkTransportUDP
	case "unix", "unixgram", "unixpacket":
		return semconvNew.NetworkTransportUnix
	default:
		return semconvNew.NetworkTransportPipe
	}
}

func (n CurrentHTTPServer) method(method string) (attribute.KeyValue, attribute.KeyValue) {
	if method == "" {
		return semconvNew.HTTPRequestMethodGet, attribute.KeyValue{}
	}
	if attr, ok := methodLookup[method]; ok {
		return attr, attribute.KeyValue{}
	}

	orig := semconvNew.HTTPRequestMethodOriginal(method)
	if attr, ok := methodLookup[strings.ToUpper(method)]; ok {
		return attr, orig
	}
	return semconvNew.HTTPRequestMethodGet, orig
}

func (n CurrentHTTPServer) scheme(https bool) attribute.KeyValue { // nolint:revive
	if https {
		return semconvNew.URLScheme("https")
	}
	return semconvNew.URLScheme("http")
}

// TraceResponse returns trace attributes for telemetry from an HTTP response.
//
// If any of the fields in the ResponseTelemetry are not set the attribute will be omitted.
func (n CurrentHTTPServer) ResponseTraceAttrs(resp ResponseTelemetry) []attribute.KeyValue {
	var count int

	if resp.ReadBytes > 0 {
		count++
	}
	if resp.WriteBytes > 0 {
		count++
	}
	if resp.StatusCode > 0 {
		count++
	}

	attributes := make([]attribute.KeyValue, 0, count)

	if resp.ReadBytes > 0 {
		attributes = append(attributes,
			semconvNew.HTTPRequestBodySize(int(resp.ReadBytes)),
		)
	}
	if resp.WriteBytes > 0 {
		attributes = append(attributes,
			semconvNew.HTTPResponseBodySize(int(resp.WriteBytes)),
		)
	}
	if resp.StatusCode > 0 {
		attributes = append(attributes,
			semconvNew.HTTPResponseStatusCode(resp.StatusCode),
		)
	}

	return attributes
}

// Route returns the attribute for the route.
func (n CurrentHTTPServer) Route(route string) attribute.KeyValue {
	return semconvNew.HTTPRoute(route)
}

func (n CurrentHTTPServer) createMeasures(meter metric.Meter) (metric.Int64Histogram, metric.Int64Histogram, metric.Float64Histogram) {
	if meter == nil {
		return noop.Int64Histogram{}, noop.Int64Histogram{}, noop.Float64Histogram{}
	}

	var err error
	requestBodySizeHistogram, err := meter.Int64Histogram(
		semconvNew.HTTPServerRequestBodySizeName,
		metric.WithUnit(semconvNew.HTTPServerRequestBodySizeUnit),
		metric.WithDescription(semconvNew.HTTPServerRequestBodySizeDescription),
	)
	handleErr(err)

	responseBodySizeHistogram, err := meter.Int64Histogram(
		semconvNew.HTTPServerResponseBodySizeName,
		metric.WithUnit(semconvNew.HTTPServerResponseBodySizeUnit),
		metric.WithDescription(semconvNew.HTTPServerResponseBodySizeDescription),
	)
	handleErr(err)
	requestDurationHistogram, err := meter.Float64Histogram(
		semconvNew.HTTPServerRequestDurationName,
		metric.WithUnit(semconvNew.HTTPServerRequestDurationUnit),
		metric.WithDescription(semconvNew.HTTPServerRequestDurationDescription),
		metric.WithExplicitBucketBoundaries(0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10),
	)
	handleErr(err)

	return requestBodySizeHistogram, responseBodySizeHistogram, requestDurationHistogram
}

func (n CurrentHTTPServer) MetricAttributes(server string, req *http.Request, statusCode int, additionalAttributes []attribute.KeyValue) []attribute.KeyValue {
	num := len(additionalAttributes) + 3
	var host string
	var p int
	if server == "" {
		host, p = SplitHostPort(req.Host)
	} else {
		// Prioritize the primary server name.
		host, p = SplitHostPort(server)
		if p < 0 {
			_, p = SplitHostPort(req.Host)
		}
	}
	hostPort := requiredHTTPPort(req.TLS != nil, p)
	if hostPort > 0 {
		num++
	}
	protoName, protoVersion := netProtocol(req.Proto)
	if protoName != "" {
		num++
	}
	if protoVersion != "" {
		num++
	}

	if statusCode > 0 {
		num++
	}

	attributes := slices.Grow(additionalAttributes, num)
	attributes = append(attributes,
		semconvNew.HTTPRequestMethodKey.String(standardizeHTTPMethod(req.Method)),
		n.scheme(req.TLS != nil),
		semconvNew.ServerAddress(host))

	if hostPort > 0 {
		attributes = append(attributes, semconvNew.ServerPort(hostPort))
	}
	if protoName != "" {
		attributes = append(attributes, semconvNew.NetworkProtocolName(protoName))
	}
	if protoVersion != "" {
		attributes = append(attributes, semconvNew.NetworkProtocolVersion(protoVersion))
	}

	if statusCode > 0 {
		attributes = append(attributes, semconvNew.HTTPResponseStatusCode(statusCode))
	}
	return attributes
}

type CurrentHTTPClient struct{}

// RequestTraceAttrs returns trace attributes for an HTTP request made by a client.
func (n CurrentHTTPClient) RequestTraceAttrs(req *http.Request) []attribute.KeyValue {
	/*
	   below attributes are returned:
	   - http.request.method
	   - http.request.method.original
	   - url.full
	   - server.address
	   - server.port
	   - network.protocol.name
	   - network.protocol.version
	*/
	numOfAttributes := 3 // URL, server address, proto, and method.

	var urlHost string
	if req.URL != nil {
		urlHost = req.URL.Host
	}
	var requestHost string
	var requestPort int
	for _, hostport := range []string{urlHost, req.Header.Get("Host")} {
		requestHost, requestPort = SplitHostPort(hostport)
		if requestHost != "" || requestPort > 0 {
			break
		}
	}

	eligiblePort := requiredHTTPPort(req.URL != nil && req.URL.Scheme == "https", requestPort)
	if eligiblePort > 0 {
		numOfAttributes++
	}
	useragent := req.UserAgent()
	if useragent != "" {
		numOfAttributes++
	}

	protoName, protoVersion := netProtocol(req.Proto)
	if protoName != "" && protoName != "http" {
		numOfAttributes++
	}
	if protoVersion != "" {
		numOfAttributes++
	}

	method, originalMethod := n.method(req.Method)
	if originalMethod != (attribute.KeyValue{}) {
		numOfAttributes++
	}

	attrs := make([]attribute.KeyValue, 0, numOfAttributes)

	attrs = append(attrs, method)
	if originalMethod != (attribute.KeyValue{}) {
		attrs = append(attrs, originalMethod)
	}

	var u string
	if req.URL != nil {
		// Remove any username/password info that may be in the URL.
		userinfo := req.URL.User
		req.URL.User = nil
		u = req.URL.String()
		// Restore any username/password info that was removed.
		req.URL.User = userinfo
	}
	attrs = append(attrs, semconvNew.URLFull(u))

	attrs = append(attrs, semconvNew.ServerAddress(requestHost))
	if eligiblePort > 0 {
		attrs = append(attrs, semconvNew.ServerPort(eligiblePort))
	}

	if protoName != "" && protoName != "http" {
		attrs = append(attrs, semconvNew.NetworkProtocolName(protoName))
	}
	if protoVersion != "" {
		attrs = append(attrs, semconvNew.NetworkProtocolVersion(protoVersion))
	}

	return attrs
}

// ResponseTraceAttrs returns trace attributes for an HTTP response made by a client.
func (n CurrentHTTPClient) ResponseTraceAttrs(resp *http.Response) []attribute.KeyValue {
	/*
	   below attributes are returned:
	   - http.response.status_code
	   - error.type
	*/
	var count int
	if resp.StatusCode > 0 {
		count++
	}

	if isErrorStatusCode(resp.StatusCode) {
		count++
	}

	attrs := make([]attribute.KeyValue, 0, count)
	if resp.StatusCode > 0 {
		attrs = append(attrs, semconvNew.HTTPResponseStatusCode(resp.StatusCode))
	}

	if isErrorStatusCode(resp.StatusCode) {
		errorType := strconv.Itoa(resp.StatusCode)
		attrs = append(attrs, semconvNew.ErrorTypeKey.String(errorType))
	}
	return attrs
}

func (n CurrentHTTPClient) ErrorType(err error) attribute.KeyValue {
	t := reflect.TypeOf(err)
	var value string
	if t.PkgPath() == "" && t.Name() == "" {
		// Likely a builtin type.
		value = t.String()
	} else {
		value = fmt.Sprintf("%s.%s", t.PkgPath(), t.Name())
	}

	if value == "" {
		return semconvNew.ErrorTypeOther
	}

	return semconvNew.ErrorTypeKey.String(value)
}

func (n CurrentHTTPClient) method(method string) (attribute.KeyValue, attribute.KeyValue) {
	if method == "" {
		return semconvNew.HTTPRequestMethodGet, attribute.KeyValue{}
	}
	if attr, ok := methodLookup[method]; ok {
		return attr, attribute.KeyValue{}
	}

	orig := semconvNew.HTTPRequestMethodOriginal(method)
	if attr, ok := methodLookup[strings.ToUpper(method)]; ok {
		return attr, orig
	}
	return semconvNew.HTTPRequestMethodGet, orig
}

func (n CurrentHTTPClient) createMeasures(meter metric.Meter) (metric.Int64Histogram, metric.Float64Histogram) {
	if meter == nil {
		return noop.Int64Histogram{}, noop.Float64Histogram{}
	}

	var err error
	requestBodySize, err := meter.Int64Histogram(
		semconvNew.HTTPClientRequestBodySizeName,
		metric.WithUnit(semconvNew.HTTPClientRequestBodySizeUnit),
		metric.WithDescription(semconvNew.HTTPClientRequestBodySizeDescription),
	)
	handleErr(err)

	requestDuration, err := meter.Float64Histogram(
		semconvNew.HTTPClientRequestDurationName,
		metric.WithUnit(semconvNew.HTTPClientRequestDurationUnit),
		metric.WithDescription(semconvNew.HTTPClientRequestDurationDescription),
		metric.WithExplicitBucketBoundaries(0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10),
	)
	handleErr(err)

	return requestBodySize, requestDuration
}

func (n CurrentHTTPClient) MetricAttributes(req *http.Request, statusCode int, additionalAttributes []attribute.KeyValue) []attribute.KeyValue {
	num := len(additionalAttributes) + 2
	var h string
	if req.URL != nil {
		h = req.URL.Host
	}
	var requestHost string
	var requestPort int
	for _, hostport := range []string{h, req.Header.Get("Host")} {
		requestHost, requestPort = SplitHostPort(hostport)
		if requestHost != "" || requestPort > 0 {
			break
		}
	}

	port := requiredHTTPPort(req.URL != nil && req.URL.Scheme == "https", requestPort)
	if port > 0 {
		num++
	}

	protoName, protoVersion := netProtocol(req.Proto)
	if protoName != "" {
		num++
	}
	if protoVersion != "" {
		num++
	}

	if statusCode > 0 {
		num++
	}

	attributes := slices.Grow(additionalAttributes, num)
	attributes = append(attributes,
		semconvNew.HTTPRequestMethodKey.String(standardizeHTTPMethod(req.Method)),
		semconvNew.ServerAddress(requestHost),
		n.scheme(req.TLS != nil),
	)

	if port > 0 {
		attributes = append(attributes, semconvNew.ServerPort(port))
	}
	if protoName != "" {
		attributes = append(attributes, semconvNew.NetworkProtocolName(protoName))
	}
	if protoVersion != "" {
		attributes = append(attributes, semconvNew.NetworkProtocolVersion(protoVersion))
	}

	if statusCode > 0 {
		attributes = append(attributes, semconvNew.HTTPResponseStatusCode(statusCode))
	}
	return attributes
}

// Attributes for httptrace.
func (n CurrentHTTPClient) TraceAttributes(host string) []attribute.KeyValue {
	return []attribute.KeyValue{
		semconvNew.ServerAddress(host),
	}
}

func (n CurrentHTTPClient) scheme(https bool) attribute.KeyValue { // nolint:revive
	if https {
		return semconvNew.URLScheme("https")
	}
	return semconvNew.URLScheme("http")
}

func isErrorStatusCode(code int) bool {
	return code >= 400 || code < 100
}
