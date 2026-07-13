package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Violation describes a single validation issue in the staging wiki.
type Violation struct {
	Path    string
	Message string
}

func (v Violation) String() string {
	if v.Path != "" {
		return fmt.Sprintf("%s: %s", v.Path, v.Message)
	}
	return v.Message
}

// WikiValidator validates staging wiki before merge to production.
type WikiValidator struct {
	maxDiffBytes int64
}

// NewWikiValidator creates a validator with the given diff budget.
func NewWikiValidator(maxDiffBytes int64) *WikiValidator {
	if maxDiffBytes <= 0 {
		maxDiffBytes = DefaultACPMaxDiffBytes
	}
	return &WikiValidator{maxDiffBytes: maxDiffBytes}
}

// Validate compares staging wiki against production wiki and returns violations.
// Returns nil if validation passes.
func (v *WikiValidator) Validate(prodDir, stagingDir string) ([]Violation, error) {
	stagingWiki := filepath.Join(stagingDir, "wiki")
	prodWiki := filepath.Join(prodDir, "wiki")

	if _, err := os.Stat(stagingWiki); os.IsNotExist(err) {
		return nil, nil // no wiki changes
	}

	var violations []Violation
	var totalDiffBytes int64

	// Walk staging wiki and check each file.
	err := filepath.Walk(stagingWiki, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(stagingWiki, path)
		if err != nil {
			return err
		}

		// Only validate .md files in wiki.
		if !strings.HasSuffix(relPath, ".md") {
			return nil
		}

		// Check valid taxonomy path.
		if !isValidWikiPath(relPath) {
			violations = append(violations, Violation{
				Path:    relPath,
				Message: "path does not conform to wiki taxonomy (semantic/, episodic/, procedural/, prospective/)",
			})
		}

		// Read staging content.
		stagingContent, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		// Check frontmatter.
		if !hasFrontmatter(string(stagingContent)) {
			violations = append(violations, Violation{
				Path:    relPath,
				Message: "missing required frontmatter (compiled_from, last_compiled)",
			})
		}

		// Calculate diff size.
		prodPath := filepath.Join(prodWiki, relPath)
		prodContent, prodErr := os.ReadFile(prodPath)
		if prodErr != nil {
			// New file — entire content counts as diff.
			totalDiffBytes += int64(len(stagingContent))
		} else {
			// Modified file — diff is the absolute size difference plus changed bytes.
			totalDiffBytes += abs64(int64(len(stagingContent)) - int64(len(prodContent)))
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk staging wiki: %w", err)
	}

	// Check diff budget.
	if totalDiffBytes > v.maxDiffBytes {
		violations = append(violations, Violation{
			Message: fmt.Sprintf("diff budget exceeded: %d bytes > %d bytes max", totalDiffBytes, v.maxDiffBytes),
		})
	}

	// Check that no production pages were deleted in staging.
	if _, err := os.Stat(prodWiki); err == nil {
		err := filepath.Walk(prodWiki, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return err
			}
			if !strings.HasSuffix(path, ".md") {
				return nil
			}

			relPath, _ := filepath.Rel(prodWiki, path)
			stagingPath := filepath.Join(stagingWiki, relPath)
			if _, err := os.Stat(stagingPath); os.IsNotExist(err) {
				violations = append(violations, Violation{
					Path:    relPath,
					Message: "page deleted in staging (ingest should not delete pages)",
				})
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("walk prod wiki: %w", err)
		}
	}

	return violations, nil
}

// isValidWikiPath checks if a path conforms to the wiki taxonomy.
func isValidWikiPath(relPath string) bool {
	validPrefixes := []string{"semantic/", "episodic/", "procedural/", "prospective/"}
	// Also allow index.md at root.
	if relPath == "index.md" {
		return true
	}
	for _, prefix := range validPrefixes {
		if strings.HasPrefix(relPath, prefix) {
			return true
		}
	}
	return false
}

// hasFrontmatter checks if wiki content has required frontmatter comments.
func hasFrontmatter(content string) bool {
	return strings.Contains(content, "compiled_from:") &&
		strings.Contains(content, "last_compiled:")
}

func abs64(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}
