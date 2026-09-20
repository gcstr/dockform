package planner

import "github.com/gcstr/dockform/internal/dockercli"

// pullProgress turns one image's layer events into a percentage that never
// decreases. Each rule below was measured against real pulls in
// internal/dockercli/testdata/composeprogress; its README has the numbers.
//
//   - A layer runs two byte-counted phases, Downloading then Extracting, each
//     0-100%. Both are counted, over twice the total, so neither phase can
//     pull the other backwards.
//   - A layer's total is only reported once it starts downloading, and docker
//     downloads a few layers at a time, so nothing is reported until every
//     announced layer has a total.
//   - A layer is announced only by "Pulling fs layer", and "Already exists"
//     un-announces it. A layer already on disk emits a single "Already exists"
//     with no total; counting it as announced would stop a percentage ever
//     appearing for any pull sharing a base layer with a cached image.
type pullProgress struct {
	announced  map[string]bool
	total      map[string]int64
	downloaded map[string]int64
	extracted  map[string]int64
	last       int
	reported   bool
}

func newPullProgress() *pullProgress {
	return &pullProgress{
		announced:  map[string]bool{},
		total:      map[string]int64{},
		downloaded: map[string]int64{},
		extracted:  map[string]int64{},
	}
}

// observe folds in one layer event. It reports a percentage only when it can
// compute one and it is higher than the last one reported.
func (p *pullProgress) observe(ev dockercli.ComposeEvent) (int, bool) {
	id := ev.Name
	if _, seen := p.total[id]; !seen && ev.Total > 0 {
		// A layer's size is fixed, so record it once. Taking a later, larger
		// total (an uncompressed size on Extracting, say) would grow the
		// denominator without growing the numerator: the percentage would drop,
		// observe would suppress it, and the reading would freeze for the rest
		// of the pull.
		p.total[id] = ev.Total
	}
	switch ev.Text {
	case "Pulling fs layer":
		p.announced[id] = true
	case "Already exists":
		// Docker can dedupe a layer mid-pull, after announcing it, against an
		// identical one already being fetched. No Downloading follows, so no
		// total ever arrives, and an announced layer without a total disables
		// the percentage for the whole image.
		delete(p.announced, id)
	case "Downloading":
		p.downloaded[id] = max(p.downloaded[id], ev.Current)
	case "Download complete":
		if t, ok := p.total[id]; ok {
			p.downloaded[id] = t
		}
	case "Extracting":
		p.extracted[id] = max(p.extracted[id], ev.Current)
	case "Pull complete":
		if t, ok := p.total[id]; ok {
			p.downloaded[id], p.extracted[id] = t, t
		}
	}

	if len(p.announced) == 0 {
		return 0, false
	}
	var sum, done int64
	for layer := range p.announced {
		t, ok := p.total[layer]
		if !ok {
			return 0, false
		}
		sum += t
		done += min(p.downloaded[layer], t) + min(p.extracted[layer], t)
	}
	if sum == 0 {
		return 0, false
	}
	pct := int(done * 100 / (2 * sum))
	if p.reported && pct <= p.last {
		return 0, false
	}
	p.last, p.reported = pct, true
	return pct, true
}
