package device

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/pkg/errors"
)

type EncoderInfo struct {
	Name   string `json:"name"`
	Codec  string `json:"codec"`
	Type   string `json:"type"`
	Vendor bool   `json:"vendor"`
}

type EncoderList struct {
	Video []EncoderInfo `json:"video"`
	Audio []EncoderInfo `json:"audio"`
}

var encoderLineRegex = regexp.MustCompile(
	`--(?:video|audio)-codec=(\S+)\s+--(?:video|audio)-encoder=(\S+)\s+\((hw|sw|hybrid)\)`,
)

// ListAndroidEncoders returns the video and audio encoders supported by an
// Android device, as reported by the scrcpy server.
func ListAndroidEncoders(deviceID string) (*EncoderList, error) {
	info, err := ParseDeviceID(deviceID)
	if err != nil {
		return nil, err
	}
	if info.Type != Android {
		return nil, fmt.Errorf("only android devices support encoders")
	}

	service, err := NewScrcpyService(info.Serial, nil)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return service.ListEncoders(ctx)
}

func (s *ScrcpyService) ListEncoders(ctx context.Context) (*EncoderList, error) {
	if err := s.pushJar(); err != nil {
		return nil, err
	}

	cmd := fmt.Sprintf(
		"CLASSPATH=/data/local/tmp/%s app_process / com.genymobile.scrcpy.Server %s list_encoders=true",
		s.getJarFileName(),
		s.version,
	)
	output, err := s.device.RunCommandContext(ctx, cmd)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list encoders")
	}

	result := parseEncoderList(output)
	if len(result.Video) == 0 && len(result.Audio) == 0 {
		return nil, errors.New("no encoders found")
	}
	return result, nil
}

func parseEncoderList(output string) *EncoderList {
	result := &EncoderList{
		Video: []EncoderInfo{},
		Audio: []EncoderInfo{},
	}

	section := ""
	for _, line := range strings.Split(output, "\n") {
		switch {
		case strings.Contains(line, "List of video encoders:"):
			section = "video"
			continue
		case strings.Contains(line, "List of audio encoders:"):
			section = "audio"
			continue
		}
		if section == "" {
			continue
		}

		match := encoderLineRegex.FindStringSubmatch(line)
		if match == nil {
			continue
		}

		encoder := EncoderInfo{
			Codec:  match[1],
			Name:   match[2],
			Type:   match[3],
			Vendor: strings.Contains(line, "[vendor]"),
		}
		if section == "video" {
			result.Video = append(result.Video, encoder)
		} else {
			result.Audio = append(result.Audio, encoder)
		}
	}

	return result
}
