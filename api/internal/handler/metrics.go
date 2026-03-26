package handler

import (
	"fmt"
	"net/http"
)

// ServeMetrics exposes P2P counters in Prometheus text format.
// No external dependency — we only have a handful of metrics.
func ServeMetrics(w http.ResponseWriter, _ *http.Request) {
	snap := P2PMetricsSnapshot()

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

	fmt.Fprintf(w, "# HELP converter_p2p_bytes_total Total bytes transferred by source.\n")
	fmt.Fprintf(w, "# TYPE converter_p2p_bytes_total counter\n")
	fmt.Fprintf(w, "converter_p2p_bytes_total{source=\"http\"} %d\n", snap["p2p_http_bytes_total"])
	fmt.Fprintf(w, "converter_p2p_bytes_total{source=\"p2p\"} %d\n", snap["p2p_p2p_bytes_total"])

	fmt.Fprintf(w, "# HELP converter_p2p_segments_total Total segments transferred by source.\n")
	fmt.Fprintf(w, "# TYPE converter_p2p_segments_total counter\n")
	fmt.Fprintf(w, "converter_p2p_segments_total{source=\"http\"} %d\n", snap["p2p_http_segments_total"])
	fmt.Fprintf(w, "converter_p2p_segments_total{source=\"p2p\"} %d\n", snap["p2p_p2p_segments_total"])

	fmt.Fprintf(w, "# HELP converter_p2p_peers Latest reported peer count.\n")
	fmt.Fprintf(w, "# TYPE converter_p2p_peers gauge\n")
	fmt.Fprintf(w, "converter_p2p_peers %d\n", snap["p2p_peers_last_snapshot"])
}
