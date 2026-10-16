// SPDX-License-Identifier: MIT

// Package service defines what the user interface can ask of the
// application: the Service interface, its data types and its errors. The
// Wails binding (internal/gui) wraps it; the engine (internal/engine)
// implements it. Nothing here imports Wails.
//
// Every type here has a TypeScript mirror in
// frontend/src/app/service/types.ts and a JSON Schema in docs/schema/.
package service

import (
	"context"
	"fmt"

	"github.com/otfabric/go-modbus-trace/trace"
)

// Service is the application as the user interface sees it.
type Service interface {
	// About returns the versions of the application and its libraries.
	About(ctx context.Context) (About, error)

	// Open prepares the trace made of paths (several files are one trace,
	// in the order given) and says whether an analysis of it is cached.
	// Nothing is read beyond the fingerprint.
	Open(ctx context.Context, paths []string) (FileInfo, error)
	// Close releases an open trace and cancels its jobs.
	Close(ctx context.Context, fileID string) error
	// Recent lists the traces opened recently, most recent first.
	Recent(ctx context.Context) ([]RecentFile, error)

	// Scan starts the first pass (decompressing a gzip trace first): the
	// file summary and the flows, for the Flows step.
	Scan(ctx context.Context, fileID string) (JobRef, error)
	// ScanResult returns what the finished scan found.
	ScanResult(ctx context.Context, fileID string) (ScanResult, error)
	// Analyze starts the second pass into the analysis cache.
	Analyze(ctx context.Context, fileID string, cfg AnalyzeConfig) (JobRef, error)
	// CancelJob cancels a running job.
	CancelJob(ctx context.Context, jobID string) error
	// Subscribe delivers job progress and completion until ctx ends.
	Subscribe(ctx context.Context) (<-chan Event, error)

	// Overview returns the facts of an analysed trace (the file strip).
	Overview(ctx context.Context, fileID string) (Overview, error)
	// Timeline returns a window of the filtered, ordered timeline.
	Timeline(ctx context.Context, fileID string, q TimelineQuery) (TimelinePage, error)
	// Detail returns one row with its decoded ADUs and the packets that
	// carried them, re-read from the trace.
	Detail(ctx context.Context, fileID string, seq uint64) (Detail, error)
	// Devices aggregates the filtered transactions per server and unit (R8).
	Devices(ctx context.Context, fileID string, f Filter) ([]DeviceStats, error)
	// Connections lists the connections with rows in the filter (R9).
	Connections(ctx context.Context, fileID string, f Filter) ([]ConnStats, error)
	// Patterns finds the recurring requests in the filter (R10).
	Patterns(ctx context.Context, fileID string, f Filter) ([]Pattern, error)
	// Coverage maps the addresses read and written of one device and data
	// space (R11).
	Coverage(ctx context.Context, fileID string, q CoverageQuery) (CoverageMap, error)
	// AddressHistory starts a job that reads the values of one address
	// from the trace; the points arrive as "history" events (R11).
	AddressHistory(ctx context.Context, fileID string, q HistoryQuery) (JobRef, error)
	// Quality lists every condition the library counted (R12).
	Quality(ctx context.Context, fileID string) (Quality, error)

	// ParseFilter parses the filter box's text (UX.md section 6); times
	// like "14:30" or "+15m" are read against the trace's first packet.
	ParseFilter(ctx context.Context, fileID, text string) (FilterParse, error)
}

// About names the versions in this build.
type About struct {
	Version       string `json:"version"`
	GoModbusTrace string `json:"go_modbus_trace"`
	Wails         string `json:"wails"`
	Go            string `json:"go"`
	OS            string `json:"os"`
	Arch          string `json:"arch"`
}

// FileInfo is an open trace.
type FileInfo struct {
	ID       string   `json:"id"`
	Paths    []string `json:"paths"`
	Name     string   `json:"name"` // the first file's base name, "+ N more" for several
	Size     int64    `json:"size"`
	Gzip     bool     `json:"gzip"`
	Analyzed bool     `json:"analyzed"` // the cache holds a complete analysis: no import needed
	// Choices remembered from the last analysis of this trace.
	Choices *AnalyzeConfig `json:"choices,omitempty"`
}

// RecentFile is an entry of the recent files list.
type RecentFile struct {
	Paths  []string `json:"paths"`
	Name   string   `json:"name"`
	Exists bool     `json:"exists"`
}

// JobRef names a started job; its progress arrives as events.
type JobRef struct {
	ID   string `json:"id"`
	Kind string `json:"kind"` // scan, analyze
}

// Job states.
const (
	JobRunning  = "running"
	JobDone     = "done"
	JobFailed   = "failed"
	JobCanceled = "canceled"
)

// JobState is the progress of a job.
type JobState struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	File    string `json:"file"`
	State   string `json:"state"`
	Phase   string `json:"phase"` // decompress, scan, analyze, index
	Read    int64  `json:"read"`  // input bytes read
	Total   int64  `json:"total"` // input bytes, -1 when unknown
	Rows    uint64 `json:"rows"`  // rows written (analyze)
	Rate    int64  `json:"rate"`  // bytes per second
	Error   *Error `json:"error,omitempty"`
	Warning string `json:"warning,omitempty"` // a finished job's caveat (for example a damaged tail)
}

// Event is what Subscribe delivers.
type Event struct {
	Kind    string        `json:"kind"` // job, history
	Job     *JobState     `json:"job,omitempty"`
	History *HistoryChunk `json:"history,omitempty"`
}

// ScanResult is the Flows step's content (R2).
type ScanResult struct {
	Summary *trace.FileSummary `json:"summary"`
	Flows   []trace.Flow       `json:"flows"`
	// ServerPorts are the server ports of the Modbus-like flows, to prefill
	// the ports field (502 is always among them).
	ServerPorts []uint16 `json:"server_ports"`
	// Selected are the flows preselected for analysis (the Modbus-like ones).
	Selected []uint32 `json:"selected"`
	// OneClick: every Modbus flow uses the same server port, so "Analyze"
	// can proceed without changes.
	OneClick bool `json:"one_click"`
}

// AnalyzeConfig is what the user confirmed in the Flows step.
type AnalyzeConfig struct {
	// Flows are the flow IDs to analyse; empty means every flow.
	Flows []uint32 `json:"flows"`
	// ServerPorts are the ports taken as servers when no handshake decides.
	ServerPorts []uint16 `json:"server_ports"`
	// Servers are endpoints ("ip:port") the user marked as servers.
	Servers []string `json:"servers"`
}

// Overview is the file strip (R4).
type Overview struct {
	File         string `json:"file"`
	Size         int64  `json:"size"`
	FirstNS      int64  `json:"first_ns"`
	LastNS       int64  `json:"last_ns"`
	Packets      uint64 `json:"packets"`
	Connections  uint64 `json:"connections"`
	Devices      int    `json:"devices"`
	Transactions uint64 `json:"transactions"`
	Rows         uint64 `json:"rows"`
	// Quality is ok, warnings or problems; QualityCount the conditions
	// behind it.
	Quality      string `json:"quality"`
	QualityCount int    `json:"quality_count"`
	// TraceMissing: the trace is not at its path any more; the overview
	// and the timeline work, detail and export do not.
	TraceMissing bool `json:"trace_missing"`
	// Summary is the library's file summary (capture facts behind R4).
	Summary *trace.FileSummary `json:"summary"`
}

// Filter is the active filter (R7). Every field narrows; an empty field
// does not.
type Filter struct {
	Servers   []string `json:"servers,omitempty"` // "ip" or "ip:port"
	Units     []uint8  `json:"units,omitempty"`
	Clients   []string `json:"clients,omitempty"` // "ip"
	Conns     []uint32 `json:"conns,omitempty"`
	Functions []uint8  `json:"functions,omitempty"`
	Statuses  []string `json:"statuses,omitempty"`
	Classes   []string `json:"classes,omitempty"`
	// Exceptions are exception codes (exception responses only).
	Exceptions []uint8  `json:"exceptions,omitempty"`
	Flags      []string `json:"flags,omitempty"`
	NotFlags   []string `json:"not_flags,omitempty"`
	FromNS     *int64   `json:"from_ns,omitempty"`
	ToNS       *int64   `json:"to_ns,omitempty"` // exclusive
	// Summary selects the transactions with exactly this summary (a
	// pattern's "show in timeline").
	Summary string `json:"summary,omitempty"`
	// Space and the address range select transactions whose range
	// overlaps; an empty Space with a range means any data space.
	Space string  `json:"space,omitempty"`
	Start *uint16 `json:"start,omitempty"`
	End   *uint16 `json:"end,omitempty"` // inclusive
}

// TimelineQuery asks for a window of the filtered, ordered timeline by
// rank (R5): the scaled scrollbar maps to ranks.
type TimelineQuery struct {
	Filter Filter `json:"filter"`
	Order  string `json:"order"` // time, latency
	Desc   bool   `json:"desc"`
	Offset uint64 `json:"offset"`
	Limit  int    `json:"limit"`
}

// TimelinePage is a window of rows.
type TimelinePage struct {
	Offset uint64 `json:"offset"`
	Rows   []Row  `json:"rows"`
	Total  uint64 `json:"total"`
}

// Row is one timeline row (R5).
type Row struct {
	Seq       uint64   `json:"seq"`
	Kind      string   `json:"kind"`
	TimeNS    int64    `json:"t_ns"`
	Conn      uint32   `json:"conn"`
	Client    string   `json:"client"`
	Server    string   `json:"server"`
	Unit      *uint8   `json:"unit,omitempty"`
	TID       *uint16  `json:"tid,omitempty"`
	FC        *uint8   `json:"fc,omitempty"`
	Class     string   `json:"class,omitempty"`
	Status    string   `json:"status,omitempty"`
	Exception uint8    `json:"exception,omitempty"`
	LatencyNS *int64   `json:"latency_ns,omitempty"`
	Flags     []string `json:"flags"`
	Summary   string   `json:"summary"`
	Result    string   `json:"result"`
}

// Detail is the detail pane of one row (R6).
type Detail struct {
	Row      Row        `json:"row"`
	Request  *ADUDetail `json:"request,omitempty"`
	Response *ADUDetail `json:"response,omitempty"`
	// Text is the one-paragraph summary in words.
	Text  string      `json:"text"`
	Notes []Note      `json:"notes"`
	Conn  *ConnDetail `json:"conn,omitempty"`
	// MBCAP says whether the transaction is exported and, if not, why.
	MBCAP string `json:"mbcap,omitempty"`
	// Extra is the event's own data for rows that are not transactions.
	Extra any `json:"extra,omitempty"`
}

// ADUDetail is a request or a response: bytes, decoded fields, packets.
type ADUDetail struct {
	// Hex is the whole ADU, MBAP header then PDU; empty when the trace
	// cannot be read (moved, compressed, or the bytes span a reassembled
	// IP datagram).
	Hex      string      `json:"hex"`
	Name     string      `json:"name"`
	Fields   []Field     `json:"fields"`
	Warnings []string    `json:"warnings"`
	Packets  []PacketRef `json:"packets"`
	FirstNS  *int64      `json:"first_ns,omitempty"`
	LastNS   *int64      `json:"last_ns,omitempty"`
}

// Field is a decoded field with its byte range in the ADU (offset 7 is the
// function code).
type Field struct {
	Name   string  `json:"name"`
	Offset int     `json:"offset"`
	Length int     `json:"length"`
	Value  *uint64 `json:"value,omitempty"`
	Hex    string  `json:"hex,omitempty"`
}

// PacketRef is a packet that carried part of an ADU.
type PacketRef struct {
	Frame   uint64 `json:"frame"` // as Wireshark numbers it, within its file
	Part    uint16 `json:"part"`  // the file, for a trace of several
	TimeNS  *int64 `json:"t_ns,omitempty"`
	Iface   uint32 `json:"interface"`
	Offset  int64  `json:"offset"` // of the record in the file
	PayOff  int32  `json:"payload_offset"`
	PayLen  int32  `json:"payload_length"`
	Display string `json:"display_filter"` // frame.number == N
}

// Note is a quality flag, a validation warning or a fact, with its
// meaning in one sentence.
type Note struct {
	Kind    string `json:"kind"` // flag, warning, fact
	Code    string `json:"code"`
	Meaning string `json:"meaning"`
}

// ConnDetail is the connection a row belongs to.
type ConnDetail struct {
	ID             uint32  `json:"id"`
	Client         string  `json:"client"`
	Server         string  `json:"server"`
	Open           string  `json:"open"`
	Close          string  `json:"close"`
	RoleSource     string  `json:"role_source"`
	RoleConfidence float64 `json:"role_confidence"`
	Transactions   uint64  `json:"transactions"`
}

// DeviceStats is one server and unit (R8).
type DeviceStats struct {
	Server       string            `json:"server"`
	Unit         uint8             `json:"unit"`
	Transactions uint64            `json:"transactions"`
	ByFunction   map[string]uint64 `json:"by_function"` // function code (decimal) -> count
	ByStatus     map[string]uint64 `json:"by_status"`
	Reads        uint64            `json:"reads"`
	Writes       uint64            `json:"writes"`
	Exceptions   uint64            `json:"exceptions"`
	Unanswered   uint64            `json:"unanswered"`
	Clients      []string          `json:"clients"`
	FirstNS      int64             `json:"first_ns"`
	LastNS       int64             `json:"last_ns"`
	Rate         float64           `json:"rate"`  // transactions per second between first and last
	Bytes        uint64            `json:"bytes"` // request and response ADU bytes
	Latency      *Distribution     `json:"latency,omitempty"`
}

// Distribution summarises durations (latencies, intervals): exact
// quantiles and a histogram of logarithmic buckets.
type Distribution struct {
	Count     uint64   `json:"count"`
	MinNS     int64    `json:"min_ns"`
	P50NS     int64    `json:"p50_ns"`
	P95NS     int64    `json:"p95_ns"`
	P99NS     int64    `json:"p99_ns"`
	MaxNS     int64    `json:"max_ns"`
	Histogram []Bucket `json:"histogram"`
}

// Bucket is one bar of a histogram: durations in [LoNS, HiNS).
type Bucket struct {
	LoNS  int64  `json:"lo_ns"`
	HiNS  int64  `json:"hi_ns"`
	Count uint64 `json:"count"`
}

// ConnStats is one connection (R9).
type ConnStats struct {
	ID             uint32   `json:"id"`
	Client         string   `json:"client"`
	Server         string   `json:"server"`
	Open           string   `json:"open"`
	Close          string   `json:"close"`
	RoleSource     string   `json:"role_source"`
	RoleConfidence float64  `json:"role_confidence"`
	RoleGuessed    bool     `json:"role_guessed"`
	FirstNS        int64    `json:"first_ns"`
	LastNS         int64    `json:"last_ns"`
	Transactions   uint64   `json:"transactions"` // in the filter
	Bytes          uint64   `json:"bytes"`        // TCP payload, both directions
	Gaps           uint64   `json:"gaps"`
	Resyncs        uint64   `json:"resyncs"`
	Flags          []string `json:"flags"` // of its transactions in the filter
}

// Pattern is a recurring request (R10): one client asking one device the
// same thing.
type Pattern struct {
	Client   string `json:"client"`
	Server   string `json:"server"`
	Unit     uint8  `json:"unit"`
	Function uint8  `json:"function"`
	Summary  string `json:"summary"`
	Space    string `json:"space,omitempty"`
	Start    uint16 `json:"start"`
	Count    uint16 `json:"count"`
	Requests uint64 `json:"requests"`
	FirstNS  int64  `json:"first_ns"`
	LastNS   int64  `json:"last_ns"`
	// Interval is the time between successive requests.
	Interval   *Distribution `json:"interval,omitempty"`
	JitterNS   int64         `json:"jitter_ns"` // median absolute deviation of the interval
	Responses  uint64        `json:"responses"`
	Changed    uint64        `json:"changed"`    // responses that differ from the one before
	Volatility float64       `json:"volatility"` // changed / (responses - 1)
	// VolatilityClass is static (never changed), slow (under 20% of the
	// answers changed) or noisy.
	VolatilityClass string `json:"volatility_class"`
}

// CoverageQuery names a device and a data space.
type CoverageQuery struct {
	Server string `json:"server"`
	Unit   uint8  `json:"unit"`
	Space  string `json:"space"`
	Filter Filter `json:"filter"`
}

// CoverageMap is what was read and written of a data space (R11).
type CoverageMap struct {
	Server  string     `json:"server"`
	Unit    uint8      `json:"unit"`
	Space   string     `json:"space"`
	Spaces  []string   `json:"spaces"` // the device's data spaces with any access
	Read    []Interval `json:"read"`
	Written []Interval `json:"written"`
}

// Interval is a merged range of addresses (inclusive) with how often and
// when it was touched.
type Interval struct {
	Start   uint16 `json:"start"`
	End     uint16 `json:"end"`
	Count   uint64 `json:"count"`
	FirstNS int64  `json:"first_ns"`
	LastNS  int64  `json:"last_ns"`
}

// HistoryQuery asks for the observed values of one address.
type HistoryQuery struct {
	Server  string `json:"server"`
	Unit    uint8  `json:"unit"`
	Space   string `json:"space"`
	Address uint16 `json:"address"`
	// After continues a capped history: the Next of the last chunk.
	After uint64 `json:"after"`
	// Limit caps the points (default 200,000).
	Limit int `json:"limit"`
	// Every keeps repeated values (every poll); otherwise only changes and
	// writes.
	Every bool `json:"every"`
}

// HistoryChunk is a batch of points of an address history job.
type HistoryChunk struct {
	Job    string         `json:"job"`
	Points []HistoryPoint `json:"points"`
	// Done: the job has finished; Next, when set, continues it ("load more").
	Done bool    `json:"done"`
	Next *uint64 `json:"next,omitempty"`
	// Candidates are the transactions that touch the address; Scanned how
	// many of them have been read.
	Candidates uint64 `json:"candidates"`
	Scanned    uint64 `json:"scanned"`
}

// HistoryPoint is one observed value: a read or a write.
type HistoryPoint struct {
	TimeNS  int64  `json:"t_ns"`
	Seq     uint64 `json:"seq"`
	Value   uint16 `json:"value"` // a register's word, or 0/1 for a bit
	Write   bool   `json:"write"`
	Client  string `json:"client"`
	Changed bool   `json:"changed"` // differs from the previous read
}

// Quality lists the conditions the library counted (R12); none is hidden
// when zero.
type Quality struct {
	Conditions []Condition `json:"conditions"`
	Facts      []Fact      `json:"facts"`
}

// Condition is one quality counter with its meaning.
type Condition struct {
	Code     string  `json:"code"`
	Label    string  `json:"label"`
	Count    uint64  `json:"count"`
	Severity string  `json:"severity"` // info, warning, problem
	Meaning  string  `json:"meaning"`
	Filter   *Filter `json:"filter,omitempty"` // "show in timeline"
}

// Fact is a capture-level fact (link types, snap length, drops, hashes).
type Fact struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// FilterParse is the parsed filter box.
type FilterParse struct {
	Filter Filter      `json:"filter"`
	Errors []TermError `json:"errors"`
}

// TermError is a term of the filter box that does not parse.
type TermError struct {
	Term    string `json:"term"`
	Message string `json:"message"`
}

// Error codes the frontend can act on.
const (
	CodeInvalidArgument = "invalid_argument"
	CodeNotFound        = "not_found"
	CodeCanceled        = "canceled"
	CodeUnavailable     = "unavailable" // the trace is missing or cannot be read
	CodeInternal        = "internal"
)

// Error is the error of a Service method, as the frontend receives it.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Err     error  `json:"-"`
}

// Error returns the message.
func (e *Error) Error() string { return e.Message }

// Unwrap returns the underlying error.
func (e *Error) Unwrap() error { return e.Err }

// Errorf creates an Error with a formatted message.
func Errorf(code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// Wrap creates an Error around err, with a formatted message in front.
func Wrap(code string, err error, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...) + ": " + err.Error(), Err: err}
}
