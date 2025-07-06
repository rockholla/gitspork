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
	id      string
	content string
}

// Integrate will process the gitspork files list to ensure integration b/w upstream -> downstream
func (i *IntegratorSharedOwnershipMerged) Integrate(filePaths []string, upstreamRepoClonePath string, downstreamRepoClonePath string, logger *Logger) error {
	for _, integratePath := range filePaths {
		logger.Log("📄 indexing upstream file %s for merged blocks", integratePath)
		file, err := os.Open(filepath.Join(upstreamRepoClonePath, integratePath))
		if err != nil {
			return fmt.Errorf("error opening upstream file %s: %v", integratePath, err)
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)

		lineNum := 0
		var currentUpstreamOwnedBlock *upstreamOwnedBlock
		var upstreamOwnedBlocks []*upstreamOwnedBlock
		for scanner.Scan() {
			lineNum = lineNum + 1
			line := scanner.Text()
			if currentUpstreamOwnedBlock == nil && strings.Contains(line, fmt.Sprintf("%s%s", gitSporkCommentMarker, sharedOwnershipMergedBeginUpstreamOwnedBlockMarker)) {
				// beginning identification of an upstream-owned block
				currentUpstreamOwnedBlock = &upstreamOwnedBlock{
					id:      strings.Split(line, gitSporkMarkerSeparator)[len(strings.Split(line, gitSporkMarkerSeparator))-1],
					content: "",
				}
				continue
			} else if currentUpstreamOwnedBlock != nil {
				if strings.Contains(line, fmt.Sprintf("%s%s", gitSporkCommentMarker, sharedOwnershipMergedEndUpstreamOwnedBlockMarker)) {
					// detected end upstream owned block, finalize this block
					upstreamOwnedBlocks = append(upstreamOwnedBlocks, currentUpstreamOwnedBlock)
					currentUpstreamOwnedBlock = nil
					continue
				}
				currentUpstreamOwnedBlock.content = fmt.Sprintf("%s%s\n", currentUpstreamOwnedBlock.content, line)
			}
		}

		if err := scanner.Err(); err != nil {
			return fmt.Errorf("error scanning/buffering upstream file %s: %v", integratePath, err)
		}

		if _, err := os.Stat(filepath.Join(downstreamRepoClonePath, integratePath)); os.IsNotExist(err) {
			if err := copyFile(filepath.Join(upstreamRepoClonePath, integratePath), filepath.Join(downstreamRepoClonePath, integratePath)); err != nil {
				return fmt.Errorf("error copying upstream %s to downstream", integratePath)
			}
		}
		logger.Log("📄 matching downstream %s contents against the upstream to merge", integratePath)
	}
	return nil
}
