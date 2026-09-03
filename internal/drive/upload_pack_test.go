package drive

import "testing"

func packMember(payload, logical int64) preparedUpload {
	return preparedUpload{payloadBytes: payload, logicalSize: logical}
}

func TestPartitionPackedUploadsHonorsMemberAndGroupLimits(t *testing.T) {
	pack := testUploadRules().Pack
	members := []preparedUpload{
		packMember(2*1024*1024, 3),
		packMember(2*1024*1024, 4),
		packMember(pack.MemberMax+1, 5),
		packMember(pack.PayloadBudget-1, 6),
		packMember(2, 7),
	}
	groups := partitionPackedUploads(members, pack)
	if len(groups) != 4 || len(groups[0].members) != 2 || len(groups[1].members) != 1 || len(groups[2].members) != 1 || len(groups[3].members) != 1 {
		t.Fatalf("groups = %#v", groups)
	}
}

func TestPartitionPackedUploadsHonorsFileLimit(t *testing.T) {
	pack := testUploadRules().Pack
	members := make([]preparedUpload, pack.MaxFiles+1)
	for index := range members {
		members[index] = packMember(1, 1)
	}
	groups := partitionPackedUploads(members, pack)
	if len(groups) != 2 || len(groups[0].members) != pack.MaxFiles || len(groups[1].members) != 1 {
		t.Fatalf("group sizes = %d, %d", len(groups[0].members), len(groups[1].members))
	}
}

func TestPackUploadURLPreservesQuery(t *testing.T) {
	got, err := packUploadURL("https://uploads.example/v2/uploads/intent-1?sig=value")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://uploads.example/v2/uploads:pack?sig=value" {
		t.Fatalf("pack URL = %q", got)
	}
	if _, err := packUploadURL("https://uploads.example/v2/other"); err == nil {
		t.Fatal("expected unresolved URL error")
	}
}

func TestPackProgressConvertsPayloadToLogicalBytes(t *testing.T) {
	members := []preparedUpload{packMember(25, 100), packMember(100, 50)}
	if got := logicalBytesForPackProgress(members, 10); got != 40 {
		t.Fatalf("logical progress = %d", got)
	}
	if got := logicalBytesForPackProgress(members, 75); got != 125 {
		t.Fatalf("logical progress = %d", got)
	}
	if got := creditedLogicalBytesForMember(members, 1, 75); got != 25 {
		t.Fatalf("member credit = %d", got)
	}
}
