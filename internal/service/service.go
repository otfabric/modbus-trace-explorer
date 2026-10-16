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

	"github.com/otfabric/go-modbus-trace/export/mbcapexport"
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
	// Prepare starts decompressing an analysed gzip trace again (FileInfo.Prepare).
	Prepare(ctx context.Context, fileID string) (JobRef, error)
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

	// PreviewMBCAP counts, from the index, what an MBCAP export would
	// write and leave out (R13).
	PreviewMBCAP(ctx context.Context, fileID string, x MBCAPExport) (MBCAPPreview, error)
	// ExportMBCAP starts a job that re-streams the trace through the
	// library into an MBCAP capture at path (R13); ExportResult has the
	// report when it is done.
	ExportMBCAP(ctx context.Context, fileID string, x MBCAPExport, path string) (JobRef, error)
	// ExportTimeline starts a job writing the timeline as CSV or JSON
	// Lines (R14).
	ExportTimeline(ctx context.Context, fileID string, x TimelineExport, path string) (JobRef, error)
	// ExportSubtrace starts a job writing the packets of the selected
	// connections as pcapng (R15).
	ExportSubtrace(ctx context.Context, fileID string, x SubtraceExport, path string) (JobRef, error)
	// ExportResult returns what a finished export wrote.
	ExportResult(ctx context.Context, jobID string) (ExportResult, error)
	// SaveReport writes a finished MBCAP export's report as JSON to path.
	SaveReport(ctx context.Context, jobID, path string) error

	// Settings returns the user's settings (R20).
	Settings(ctx context.Context) (Settings, error)
	// SaveSettings stores the settings and returns them as kept.
	SaveSettings(ctx context.Context, s Settings) (Settings, error)
	// CacheInfo returns the analysis cache's location and size.
	CacheInfo(ctx context.Context) (CacheInfo, error)
	// ClearCache removes every cache entry not in use.
	ClearCache(ctx context.Context) (CacheInfo, error)
	// ClearRecent empties the recent files list.
	ClearRecent(ctx context.Context) error
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
	// Prepare: an analysed gzip trace whose decompressed copy was removed
	// when it was last closed (A16); Prepare makes it again before the
	// bytes can be read.
	Prepare bool `json:"prepare,omitempty"`
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

// MBCAPExport is the MBCAP export dialog (R13).
type MBCAPExport struct {
	// Filter selects the transactions: everything ({}), the current
	// filter, or one device; its time range is the export window. The
	// library's selection has no exception-code or request terms.
	Filter     Filter            `json:"filter"`
	Exceptions bool              `json:"exceptions"` // write exception responses (default on)
	ChangeOnly bool              `json:"change_only"`
	TickNS     uint64            `json:"tick_ns"`          // 0: the library's default
	Labels     map[string]string `json:"labels,omitempty"` // "ip:port/unit" -> label
	Format     string            `json:"format"`           // mbcap, jsonl or csv
}

// MBCAPPreview is what an export would write, counted from the index.
type MBCAPPreview struct {
	Selection    string `json:"selection"` // the library's canonical form; "" is everything
	Transactions uint64 `json:"transactions"`
	Samples      uint64 `json:"samples"`
	// UpTo: Samples is an upper bound (change-only drops unchanged
	// responses, which only the export can tell).
	UpTo     bool            `json:"up_to"`
	Targets  []PreviewTarget `json:"targets"`
	Excluded []Exclusion     `json:"excluded"`
	Notes    []string        `json:"notes"`
}

// PreviewTarget is one target of a previewed export.
type PreviewTarget struct {
	Key     string   `json:"key"` // ip:port/unit, the label's key
	Server  string   `json:"server"`
	Unit    uint8    `json:"unit"`
	Label   string   `json:"label"`
	Samples uint64   `json:"samples"`
	Clients []string `json:"clients"` // one stream each
}

// Exclusion counts the transactions an export leaves out for one reason.
type Exclusion struct {
	Reason  string `json:"reason"`
	Count   uint64 `json:"count"`
	Meaning string `json:"meaning"`
}

// TimelineExport is the timeline export (R14).
type TimelineExport struct {
	Filter Filter `json:"filter"`
	Format string `json:"format"` // csv or jsonl
}

// SubtraceExport is the sub-trace export (R15): connections (by conn,
// server or client) and a time window; other filter terms are refused.
type SubtraceExport struct {
	Filter Filter `json:"filter"`
}

// ExportResult is what a finished export wrote.
type ExportResult struct {
	Job    string `json:"job"`
	Kind   string `json:"kind"` // mbcap, timeline, subtrace
	File   string `json:"file"` // the file name, without its directory
	Format string `json:"format,omitempty"`
	// MBCAP: the library's report, and the replay hand-off (R16).
	Report  *mbcapexport.Report `json:"report,omitempty"`
	Replay  []string            `json:"replay,omitempty"`  // commands to copy
	Caveats []string            `json:"caveats,omitempty"` // what a replay does not do
	Doc     string              `json:"doc,omitempty"`     // the replay walkthrough (an https link)
	// Timeline: rows written, and rows written out of time order.
	Rows uint64 `json:"rows,omitempty"`
	Late uint64 `json:"late,omitempty"`
	// Sub-trace.
	Packets     uint64 `json:"packets,omitempty"`
	Bytes       uint64 `json:"bytes,omitempty"`
	Connections int    `json:"connections,omitempty"`
}

// Settings are the user's choices (R20). The theme is kept by the window
// (it applies before the engine answers).
type Settings struct {
	TimeMode    string   `json:"time_mode"`    // local, utc, relative
	ServerPorts []uint16 `json:"server_ports"` // added to the Flows step's suggestion
	OneBased    bool     `json:"one_based"`    // address labels 1-based
	CacheCap    int64    `json:"cache_cap"`    // bytes
}

// CacheInfo describes the analysis cache.
type CacheInfo struct {
	Dir     string `json:"dir"`
	Bytes   int64  `json:"bytes"`
	Cap     int64  `json:"cap"`
	Entries int    `json:"entries"`
}
