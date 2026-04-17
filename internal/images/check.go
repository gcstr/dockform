package images

import (
	"context"
	"regexp"
	"sort"
	"sync"

	"github.com/Masterminds/semver/v3"
	"github.com/gcstr/dockform/internal/registry"
)

// Check scans all images across the provided inputs and compares them against
// registries for digest staleness and newer semver tags.
//
// Errors for individual images are captured in ImageStatus.Error so that a
// single failing image does not abort the entire check.
// All images are checked concurrently; output order matches input order.
func Check(ctx context.Context, inputs []CheckInput, reg registry.Registry, localDigestFn LocalDigestFunc) ([]ImageStatus, error) {
	type job struct {
		input   CheckInput
		svcName string
	}

	// Flatten into an ordered slice so results can be written at fixed indices.
	var jobs []job
	for _, input := range inputs {
		svcNames := make([]string, 0, len(input.Services))
		for name := range input.Services {
			svcNames = append(svcNames, name)
		}
		sort.Strings(svcNames)
		for _, svcName := range svcNames {
			jobs = append(jobs, job{input, svcName})
		}
	}

	results := make([]ImageStatus, len(jobs))

	var wg sync.WaitGroup
	wg.Add(len(jobs))
	for i, j := range jobs {
		go func(idx int, j job) {
			defer wg.Done()
			results[idx] = checkImage(ctx, j.input.StackKey, j.svcName, j.input.Services[j.svcName], j.input.TagPattern, reg, localDigestFn)
		}(i, j)
	}
	wg.Wait()

	return results, nil
}

func checkImage(
	ctx context.Context,
	stackKey, svcName, imageStr, tagPattern string,
	reg registry.Registry,
	localDigestFn LocalDigestFunc,
) ImageStatus {
	status := ImageStatus{
		Stack:         stackKey,
		Service:       svcName,
		Image:         imageStr,
		HasTagPattern: tagPattern != "",
	}

	ref, err := registry.ParseImageRef(imageStr)
	if err != nil {
		status.Error = err.Error()
		return status
	}
	status.CurrentTag = ref.Tag

	// Compare digests.
	remoteDigest, err := reg.GetRemoteDigest(ctx, ref, ref.Tag)
	if err != nil {
		status.Error = err.Error()
		return status
	}

	localDigest, err := localDigestFn(ctx, stackKey, svcName, imageStr)
	if err != nil {
		status.Error = err.Error()
		return status
	}

	status.DigestStale = remoteDigest != localDigest

	// Tag comparison (only when a pattern is configured).
	if tagPattern == "" {
		return status
	}

	newerTags, err := findNewerTags(ctx, reg, ref, tagPattern)
	if err != nil {
		status.Error = err.Error()
		return status
	}
	status.NewerTags = newerTags

	return status
}

// findNewerTags lists remote tags, filters by the regex pattern, parses them as
// semver, and returns those newer than the current tag sorted descending.
// If the current tag is not valid semver, it returns nil (digest-only).
func findNewerTags(ctx context.Context, reg registry.Registry, ref registry.ImageRef, tagPattern string) ([]string, error) {
	currentVer, err := semver.NewVersion(ref.Tag)
	if err != nil {
		// Current tag is not valid semver — skip tag comparison.
		return nil, nil
	}

	re, err := regexp.Compile(tagPattern)
	if err != nil {
		return nil, err
	}

	allTags, err := reg.ListTags(ctx, ref)
	if err != nil {
		return nil, err
	}

	type tagVersion struct {
		tag string
		ver *semver.Version
	}

	var candidates []tagVersion
	for _, t := range allTags {
		if !re.MatchString(t) {
			continue
		}
		v, parseErr := semver.NewVersion(t)
		if parseErr != nil {
			continue
		}
		if v.GreaterThan(currentVer) {
			candidates = append(candidates, tagVersion{tag: t, ver: v})
		}
	}

	// Sort descending (newest first).
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].ver.GreaterThan(candidates[j].ver)
	})

	newer := make([]string, 0, len(candidates))
	for _, c := range candidates {
		newer = append(newer, c.tag)
	}

	return newer, nil
}
