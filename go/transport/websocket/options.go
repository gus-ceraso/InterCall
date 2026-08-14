package websocket

import (
	"net/http"

	coderws "github.com/coder/websocket"
)

// DialOptions configures DialStream. Zero values select the safe defaults.
type DialOptions struct {
	HTTPClient   *http.Client
	HTTPHeader   http.Header
	MessageLimit int64
	Compression  bool
}

// AcceptOptions configures AcceptStream. Zero values use same-origin checks,
// disabled compression, and DefaultMessageLimit.
type AcceptOptions struct {
	OriginPatterns     []string
	InsecureSkipOrigin bool
	MessageLimit       int64
	Compression        bool
}

func (o *DialOptions) clone() *DialOptions {
	if o == nil {
		return &DialOptions{}
	}
	copy := *o
	copy.HTTPHeader = o.HTTPHeader.Clone()
	return &copy
}

func (o *AcceptOptions) clone() *AcceptOptions {
	if o == nil {
		return &AcceptOptions{}
	}
	copy := *o
	copy.OriginPatterns = append([]string(nil), o.OriginPatterns...)
	return &copy
}

func (o *DialOptions) coderOptions() *coderws.DialOptions {
	compression := coderws.CompressionDisabled
	if o.Compression {
		compression = coderws.CompressionNoContextTakeover
	}
	return &coderws.DialOptions{
		HTTPClient:      o.HTTPClient,
		HTTPHeader:      o.HTTPHeader,
		CompressionMode: compression,
	}
}

func (o *AcceptOptions) coderOptions() *coderws.AcceptOptions {
	compression := coderws.CompressionDisabled
	if o.Compression {
		compression = coderws.CompressionNoContextTakeover
	}
	return &coderws.AcceptOptions{
		OriginPatterns:     o.OriginPatterns,
		InsecureSkipVerify: o.InsecureSkipOrigin,
		CompressionMode:    compression,
	}
}
