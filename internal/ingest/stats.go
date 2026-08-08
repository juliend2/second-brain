package ingest

import "fmt"

// Stats summarizes one ingest run.
type Stats struct {
	Pages   int // nodes examined
	New     int // newly created nodes
	Updated int // existing nodes whose content changed
	Skipped int // unchanged nodes skipped
	Failed  int // nodes that could not be processed
}

func (s Stats) String() string {
	return fmt.Sprintf("pages=%d new=%d updated=%d skipped=%d failed=%d",
		s.Pages, s.New, s.Updated, s.Skipped, s.Failed)
}
