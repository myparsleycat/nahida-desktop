package drive

import (
	"errors"
	"net/url"
	"regexp"
)

var uploadIntentURLPattern = regexp.MustCompile(`/uploads/[^/]+$`)

type preparedUpload struct {
	upload UploadPlanEntry
	source FinalUploadFile
	copies []FinalUploadFile
	// payload is never packed when it is streamed.
	payload     uploadPayload
	logicalSize int64
	// held is the share of the run's payload budget the member keeps until its
	// upload settles.
	held int64
	// recoverable marks a member whose storage failures a recovery pass replans.
	recoverable bool
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
		if member.payload.streamed || member.payload.size > pack.MemberMax {
			flush()
			groups = append(groups, packedUploadGroup{members: []preparedUpload{member}})
			continue
		}
		if len(current) > 0 && (len(current) >= pack.MaxFiles || bytes+member.payload.size > pack.PayloadBudget) {
			flush()
		}
		current = append(current, member)
		bytes += member.payload.size
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
		end := cursor + member.payload.size
		cursor = end
		switch {
		case uploadedPayload >= end:
			credited += member.logicalSize
		case uploadedPayload > start && member.payload.size > 0:
			credited += member.logicalSize * (uploadedPayload - start) / member.payload.size
		}
	}
	return credited
}

// sentPackMembers counts the leading members whose payload was sent in full.
func sentPackMembers(members []preparedUpload, uploadedPayload int64) int {
	var cursor int64
	for index, member := range members {
		cursor += member.payload.size
		if uploadedPayload < cursor {
			return index
		}
	}
	return len(members)
}

func creditedLogicalBytesForMember(members []preparedUpload, memberIndex int, uploadedPayload int64) int64 {
	if memberIndex < 0 || memberIndex >= len(members) {
		return 0
	}
	var start int64
	for index := range memberIndex {
		start += members[index].payload.size
	}
	member := members[memberIndex]
	if uploadedPayload <= start {
		return 0
	}
	if member.payload.size <= 0 || uploadedPayload >= start+member.payload.size {
		return member.logicalSize
	}
	return member.logicalSize * (uploadedPayload - start) / member.payload.size
}
