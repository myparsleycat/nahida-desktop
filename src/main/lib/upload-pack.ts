export const MAX_UPLOAD_BODY_BYTES = 100 * 1024 * 1024;
export const PACK_PAYLOAD_BUDGET = 90 * 1024 * 1024;
export const PACK_MEMBER_MAX = 4 * 1024 * 1024;
export const PACK_MAX_FILES = 100;
export const DIRECT_UPLOAD_THRESHOLD = 80 * 1024 * 1024;

export type PackedUploadGroup<T extends { payloadBytes: number }> =
    | { kind: "pack"; members: T[] }
    | { kind: "single"; member: T };

export function partitionPackedUploads<T extends { payloadBytes: number }>(
    members: T[],
): PackedUploadGroup<T>[] {
    const groups: T[][] = [];
    let current: T[] = [];
    let bytes = 0;
    for (const member of members) {
        if (member.payloadBytes > PACK_MEMBER_MAX) {
            if (current.length > 0) {
                groups.push(current);
                current = [];
                bytes = 0;
            }
            groups.push([member]);
            continue;
        }
        if (
            current.length > 0 &&
            (current.length >= PACK_MAX_FILES || bytes + member.payloadBytes > PACK_PAYLOAD_BUDGET)
        ) {
            groups.push(current);
            current = [];
            bytes = 0;
        }
        current.push(member);
        bytes += member.payloadBytes;
    }
    if (current.length > 0) groups.push(current);

    return groups.map((group) =>
        group.length === 1
            ? { kind: "single" as const, member: group[0] }
            : { kind: "pack" as const, members: group },
    );
}

export function packUploadUrl(intentUrl: string) {
    const packed = intentUrl.replace(/\/uploads\/[^/?#]+(?=[?#]|$)/, "/uploads:pack");
    if (packed === intentUrl) throw new Error("pack_url_unresolved");
    return packed;
}

export function logicalBytesForPackProgress(
    members: Array<{ logicalSize: number; payloadBytes: number }>,
    uploadedPayload: number,
) {
    let credited = 0;
    let cursor = 0;
    for (const member of members) {
        const start = cursor;
        const end = cursor + member.payloadBytes;
        cursor = end;
        if (uploadedPayload >= end) credited += member.logicalSize;
        else if (uploadedPayload > start && member.payloadBytes > 0) {
            credited += Math.floor(
                (member.logicalSize * (uploadedPayload - start)) / member.payloadBytes,
            );
        }
    }
    return credited;
}

export function creditedLogicalBytesForMember(
    members: Array<{ logicalSize: number; payloadBytes: number }>,
    memberIndex: number,
    uploadedPayload: number,
) {
    const member = members[memberIndex];
    if (!member) return 0;
    const start = members.slice(0, memberIndex).reduce((sum, item) => sum + item.payloadBytes, 0);
    if (uploadedPayload <= start) return 0;
    if (member.payloadBytes <= 0 || uploadedPayload >= start + member.payloadBytes) {
        return member.logicalSize;
    }
    return Math.floor((member.logicalSize * (uploadedPayload - start)) / member.payloadBytes);
}
