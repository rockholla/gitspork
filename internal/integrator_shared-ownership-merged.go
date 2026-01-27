package internal

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	sharedOwnershipMergedBeginUpstreamOwnedBlockMarker string = "begin-upstream-owned-block"
	sharedOwnershipMergedEndUpstreamOwnedBlockMarker   string = "end-upstream-owned-block"
)

// IntegratorSharedOwnershipMerged will process a list of files to have shared ownership and generic merging based on blocks defined as owned by the upstream repo
type IntegratorSharedOwnershipMerged struct{}

type upstreamOwnedBlock struct {
	beginMarker string
	content     string
	endMarker   string
}

// Integrate will process the gitspork files list to ensure integration b/w upstream -> downstream
func (i *IntegratorSharedOwnershipMerged) Integrate(configuredGlobPatterns []string, upstreamPath string, downstreamPath string, logger *Logger) error {
	integrateFiles, err := getIntegrateFiles(upstreamPath, configuredGlobPatterns)
	if err != nil {
		return fmt.Errorf("error determining the list of files to integrate in %s from %v: %v", upstreamPath, configuredGlobPatterns, err)
	}
	for _, integrateFile := range integrateFiles {
		logger.Log("➰ parsing upstream file %s for owned blocks", integrateFile)
		upstreamFile, err := os.Open(filepath.Join(upstreamPath, integrateFile))
		if err != nil {
			return fmt.Errorf("error opening upstream file %s: %v", integrateFile, err)
		}
		defer upstreamFile.Close()

		upstreamScanner := bufio.NewScanner(upstreamFile)

		var currentUpstreamOwnedBlock *upstreamOwnedBlock
		var upstreamOwnedBlocks []*upstreamOwnedBlock
		for upstreamScanner.Scan() {
			line := upstreamScanner.Text()
			if currentUpstreamOwnedBlock == nil {
				// not currently tracking/assembling an upstream-owned block
				if strings.Contains(line, fmt.Sprintf("%s%s", gitSporkCommentMarker, sharedOwnershipMergedBeginUpstreamOwnedBlockMarker)) {
					// beginning identification of an upstream-owned block
					currentUpstreamOwnedBlock = &upstreamOwnedBlock{
						beginMarker: line,
						content:     "",
					}
					continue
				}
			} else {
				// currently tracking/assembling an upstream-owned block
				if strings.Contains(line, fmt.Sprintf("%s%s", gitSporkCommentMarker, sharedOwnershipMergedEndUpstreamOwnedBlockMarker)) {
					// detected end upstream owned block, finalize this block
					currentUpstreamOwnedBlock.endMarker = line
					upstreamOwnedBlocks = append(upstreamOwnedBlocks, currentUpstreamOwnedBlock)
					currentUpstreamOwnedBlock = nil
					continue
				}
				currentUpstreamOwnedBlock.content = fmt.Sprintf("%s%s\n", currentUpstreamOwnedBlock.content, line)
			}
		}

		if err := upstreamScanner.Err(); err != nil {
			return fmt.Errorf("error scanning/buffering upstream file %s: %v", integrateFile, err)
		}

		if _, err := os.Stat(filepath.Join(downstreamPath, integrateFile)); os.IsNotExist(err) {
			if err := syncFile(filepath.Join(upstreamPath, integrateFile), filepath.Join(downstreamPath, integrateFile)); err != nil {
				return fmt.Errorf("error copying upstream %s to downstream", integrateFile)
			}
		}

		logger.Log("🔧 merging upstream file owned blocks from %s into downstream ", integrateFile)
		mergedContent := ""
		downstreamFile, err := os.Open(filepath.Join(downstreamPath, integrateFile))
		if err != nil {
			return fmt.Errorf("error opening downstream file %s: %v", integrateFile, err)
		}
		defer downstreamFile.Close()

		downstreamScanner := bufio.NewScanner(downstreamFile)

		waitingForUpstreamOwnedBlockEnd := false
		for downstreamScanner.Scan() {
			line := downstreamScanner.Text()
			if waitingForUpstreamOwnedBlockEnd {
				// we're continuing to silently bypass lines in the downstream in this case, as the block has been replaced
				// from the relevant upstream defined block
				if strings.Contains(line, fmt.Sprintf("%s%s", gitSporkCommentMarker, sharedOwnershipMergedEndUpstreamOwnedBlockMarker)) {
					waitingForUpstreamOwnedBlockEnd = false
					continue
				}
			} else if strings.Contains(line, fmt.Sprintf("%s%s", gitSporkCommentMarker, sharedOwnershipMergedBeginUpstreamOwnedBlockMarker)) {
				// found begin owned block begin, we can simply inject the upstream-defined owned block at the same index and then just
				// continue scanning the downstream file until we see the next end upstream owned block marker
				mergedContent = fmt.Sprintf("%s%s\n%s%s\n",
					mergedContent,
					upstreamOwnedBlocks[0].beginMarker,
					upstreamOwnedBlocks[0].content,
					upstreamOwnedBlocks[0].endMarker,
				)
				waitingForUpstreamOwnedBlockEnd = true
				upstreamOwnedBlocks = upstreamOwnedBlocks[1:] // shifting the first element off the slice, previous second item becomes new first
				continue
			} else {
				// every other case we should simply be merging the dowstream line back into merged content
				mergedContent = fmt.Sprintf("%s%s\n", mergedContent, line)
			}
		}
		// if we still have upstream owned blocks in our slice/list, we can just begin appending them here
		for _, upstreamOwnedBlock := range upstreamOwnedBlocks {
			mergedContent = fmt.Sprintf("%s%s\n%s%s\n",
				mergedContent,
				upstreamOwnedBlock.beginMarker,
				upstreamOwnedBlock.content,
				upstreamOwnedBlock.endMarker,
			)
		}

		if err := downstreamScanner.Err(); err != nil {
			return fmt.Errorf("error scanning/buffering downstream file %s: %v", integrateFile, err)
		}

		if err := os.WriteFile(filepath.Join(downstreamPath, integrateFile), []byte(mergedContent), 0644); err != nil {
			return fmt.Errorf("error writing merged file %s to downstream: %v", integrateFile, err)
		}
	}

	return nil
}
