package main

import (
	"bytes"
	"io"
	"regexp"
	"sort"
)

// InternalScanner handles the secret detection logic
type InternalScanner struct {
	patterns map[string]*regexp.Regexp
}

func NewInternalScanner() *InternalScanner {
	return &InternalScanner{
		patterns: map[string]*regexp.Regexp{
			"NVIDIA API Key":      regexp.MustCompile(`nvapi-[A-Za-z0-9_-]{64}`),
			"OpenRouter API Key":  regexp.MustCompile(`sk-or-v1-[A-Za-z0-9]{64}`),
			"GCP API Key":         regexp.MustCompile(`AIza[0-9A-Za-z\\-_]{35}`),
			"AWS Access Key ID":   regexp.MustCompile(`(AKIA|ASIA|AGPA|AIDA|AROA|AIPA|ANPA|ANVA|A3T[A-Z0-9])[A-Z0-9]{16}`),
			"AWS Secret Key":      regexp.MustCompile(`(?i)aws_secret_access_key\s*[:=]\s*["']?([A-Za-z0-9/+=]{40})["']?`),
			"Digital Ocean Token": regexp.MustCompile(`dop_v1_[a-f0-9]{64}`),
			"Azure API Key":       regexp.MustCompile(`(?i)(?:azure|openai|cognitive).{0,20}[:=]\s*["']?([a-fA-F0-9]{32})["']?`),
			"OpenAI API Key":      regexp.MustCompile(`sk-[a-zA-Z0-9]{48}`),
			"GitHub PAT":          regexp.MustCompile(`(?:ghp|gho|ghu|ghs|ghr)_[a-zA-Z0-9]{36}`),
			"GitHub App Token":    regexp.MustCompile(`(ghv|ghb)_[a-zA-Z0-9]{36}`),
			"Google OAuth":        regexp.MustCompile(`"client_secret":"[a-zA-Z0-9-_]{24}"`),
			"Stripe API Key":      regexp.MustCompile(`(?:sk|pk)_(?:test|live)_[0-9a-zA-Z]{24}`),
			"Slack Token":         regexp.MustCompile(`xox[baprs]-[0-9a-zA-Z]{10,48}`),
			"Generic Secret":      regexp.MustCompile(`(?i)(?:secret|token|password|key)[^\x00\s]{0,10}[:=]\s*["']?([A-Za-z0-9_\-\.]{16,})["']?`),
			"Private Key":         regexp.MustCompile(`-----BEGIN (?:RSA|EC|PGP|OPENSSH) PRIVATE KEY-----`),
		},
	}
}

func (is *InternalScanner) Scan(reader io.Reader) (ScanResult, error) {
	const chunkSize = 256 * 1024
	const overlapSize = 4096

	buffer := make([]byte, chunkSize+overlapSize)
	var globalOffset int64 = 0
	var totalLines int = 1

	type matchKey struct {
		offset int64
		last4  string
	}
	foundMap := make(map[matchKey]FoundKey)

	for i := 0; i < overlapSize; i++ {
		buffer[i] = 0
	}

	for {
		n, err := io.ReadFull(reader, buffer[overlapSize:])
		if n == 0 && (err == io.EOF || err == io.ErrUnexpectedEOF) {
			break
		}

		data := buffer[:overlapSize+n]

		for name, re := range is.patterns {
			matches := re.FindAllSubmatchIndex(data, -1)
			for _, m := range matches {
				start, end := m[0], m[1]
				if len(m) >= 4 {
					s, e := m[len(m)-2], m[len(m)-1]
					if e-s > 8 {
						start, end = s, e
					}
				}

				if end <= overlapSize {
					continue
				}

				key := string(data[start:end])
				if len(key) < 8 {
					continue
				}

				absOffset := globalOffset + int64(start) - overlapSize
				last4 := truncateKey(key)

				mk := matchKey{offset: absOffset, last4: last4}
				existing, exists := foundMap[mk]

				if !exists || (existing.Type == "Generic Secret" && name != "Generic Secret") {
					lineNo := totalLines
					if start < overlapSize {
						lineNo -= bytes.Count(data[start:overlapSize], []byte{'\n'})
					} else {
						lineNo += bytes.Count(data[overlapSize:start], []byte{'\n'})
					}

					foundMap[mk] = FoundKey{
						Type:   name,
						Last4:  last4,
						LineNo: lineNo,
						Offset: absOffset,
					}
				}
			}
		}

		totalLines += bytes.Count(buffer[overlapSize:overlapSize+n], []byte{'\n'})
		globalOffset += int64(n)

		if n >= chunkSize {
			copy(buffer[:overlapSize], buffer[chunkSize:chunkSize+overlapSize])
		}

		if err == io.EOF || err == io.ErrUnexpectedEOF {
			break
		}
	}

	found := make([]FoundKey, 0, len(foundMap))
	for _, fk := range foundMap {
		found = append(found, fk)
	}
	sort.Slice(found, func(i, j int) bool {
		return found[i].Offset < found[j].Offset
	})

	return ScanResult{
		ExposedKeys: found,
		Status:      "Success",
	}, nil
}

func truncateKey(key string) string {
	if len(key) <= 4 {
		return key
	}
	return key[len(key)-4:]
}
