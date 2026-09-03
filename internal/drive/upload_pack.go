package drive

import (
	"errors"
	"net/url"
	"regexp"
)

var uploadIntentURLPattern = regexp.MustCompile(`/uploads/[^/]+$`)

type preparedUpload struct {
	upload       UploadPlanEntry
	source       FinalUploadFile
	copies       []FinalUploadFile
	data         []byte
	compression  string
	payloadBytes int64
	logicalSize  int64
}

type packedUploadGroup struct {
	members []preparedUpload
}

func partitionPackedUploads(members []preparedUpload, pack UploadPackRules) []packedUploadGroup {
	groups := make([]packedUploadGroup, 0)
	current := make([]preparedUpload, 0, max(1, pack.MaxFiles))
	var bytes int64
	flush := func() {
		if len(current) == 0 {
			return
		}
		groups = append(groups, packedUploadGroup{members: current})
		current = make([]preparedUpload, 0, max(1, pack.MaxFiles))
		bytes = 0
	}
	for _, member := range members {
		if member.payloadBytes > pack.MemberMax {
			flush()
			groups = append(groups, packedUploadGroup{members: []preparedUpload{member}})
			continue
		}
		if len(current) > 0 && (len(current) >= pack.MaxFiles || bytes+member.payloadBytes > pack.PayloadBudget) {
			flush()
		}
		current = append(current, member)
		bytes += member.payloadBytes
	}
	flush()
	return groups
}

func packUploadURL(intentURL string) (string, error) {
	parsed, err := url.Parse(intentURL)
	if err != nil {
		return "", err
	}
	packedPath := uploadIntentURLPattern.ReplaceAllString(parsed.Path, "/uploads:pack")
	if packedPath == parsed.Path {
		return "", errors.New("pack_url_unresolved")
	}
	parsed.Path = packedPath
	parsed.RawPath = ""
	return parsed.String(), nil
}

func logicalBytesForPackProgress(members []preparedUpload, uploadedPayload int64) int64 {
	var credited int64
	var cursor int64
	for _, member := range members {
		start := cursor
		end := cursor + member.payloadBytes
		cursor = end
		switch {
		case uploadedPayload >= end:
			credited += member.logicalSize
		case uploadedPayload > start && member.payloadBytes > 0:
			credited += member.logicalSize * (uploadedPayload - start) / member.payloadBytes
		}
	}
	return credited
}

func creditedLogicalBytesForMember(members []preparedUpload, memberIndex int, uploadedPayload int64) int64 {
	if memberIndex < 0 || memberIndex >= len(members) {
		return 0
	}
	var start int64
	for index := range memberIndex {
		start += members[index].payloadBytes
	}
	member := members[memberIndex]
	if uploadedPayload <= start {
		return 0
	}
	if member.payloadBytes <= 0 || uploadedPayload >= start+member.payloadBytes {
		return member.logicalSize
	}
	return member.logicalSize * (uploadedPayload - start) / member.payloadBytes
}
