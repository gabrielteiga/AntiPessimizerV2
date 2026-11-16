package live

import "github.com/gabrielteiga/AntiPessimizerV2/internal/platform"

// sampleHeap reorders CPU-buffer deliveries before advancing window watermarks.
// Only the owner of Run touches it; bounded separately from the callback queue.
type sampleHeap []platform.Sample

func (h sampleHeap) Len() int           { return len(h) }
func (h sampleHeap) Less(i, j int) bool { return h[i].At.Before(h[j].At) }
func (h sampleHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *sampleHeap) Push(v any)        { *h = append(*h, v.(platform.Sample)) }
func (h *sampleHeap) Pop() any {
	n := len(*h) - 1
	v := (*h)[n]
	(*h)[n] = platform.Sample{}
	*h = (*h)[:n]
	return v
}
