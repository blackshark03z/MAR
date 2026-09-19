package worker

// Worker-visible coding tool names are executed by the ACI runtime. These
// compatibility declarations intentionally contain no execution path: they
// keep worker protocol naming explicit without creating a second tool runtime.
const (
	ReplaceExactToolName     = "replace_exact"
	ReplaceManyExactToolName = "replace_many_exact"
)

// ReplaceManyExactReceiptContract documents the hash boundary exposed by the
// ACI compound operation: one starting expected_sha256 and one final_sha256.
type ReplaceManyExactReceiptContract struct {
	ExpectedSHA256 string `json:"expected_sha256"`
	FinalSHA256    string `json:"final_sha256"`
}
