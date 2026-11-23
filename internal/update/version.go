package update

import (
	"regexp"
	"strconv"
	"strings"
)

func isNewer(latest, current string) bool {
	latest = stripGitDescribe(strings.TrimSpace(latest))
	current = stripGitDescribe(strings.TrimSpace(current))
	lv, ok := parseVersion(latest)
	if !ok {
		return false
	}
	cv, ok := parseVersion(current)
	if !ok {
		return false
	}
	for i := 0; i < 3; i++ {
		if lv.nums[i] != cv.nums[i] {
			return lv.nums[i] > cv.nums[i]
		}
	}
	if lv.pre == cv.pre {
		return false
	}
	if lv.pre == "" {
		return true
	}
	if cv.pre == "" {
		return false
	}
	return lv.pre > cv.pre
}

func isSemver(value string) bool {
	_, ok := parseVersion(value)
	return ok
}

var gitDescribePattern = regexp.MustCompile(`-\d+-g[0-9a-f]+(?:-dirty)?$`)

// stripGitDescribe turns versions produced by `git describe` (for example
// 1.0.4-2-gabc123) into their base release version for comparison.
func stripGitDescribe(value string) string {
	if match := gitDescribePattern.FindStringIndex(value); match != nil {
		return value[:match[0]]
	}
	return value
}

func isDevVersion(value string) bool {
	value = strings.TrimSpace(value)
	if !isSemver(value) {
		return true
	}
	return strings.Contains(value, "-dirty") || gitDescribePattern.MatchString(value)
}

type version struct {
	nums [3]int
	pre  string
}

func parseVersion(value string) (version, bool) {
	value = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(value), "v"))
	if value == "" {
		return version{}, false
	}
	if i := strings.IndexByte(value, '+'); i >= 0 {
		value = value[:i]
	}
	pre := ""
	if i := strings.IndexByte(value, '-'); i >= 0 {
		pre = value[i+1:]
		value = value[:i]
	}
	parts := strings.Split(value, ".")
	if len(parts) == 0 || len(parts) > 3 {
		return version{}, false
	}
	var out version
	for i, part := range parts {
		if part == "" {
			return version{}, false
		}
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return version{}, false
		}
		out.nums[i] = n
	}
	out.pre = pre
	return out, true
}
