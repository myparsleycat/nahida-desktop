export function GetFSEntries(items: DataTransferItemList) {
    return Array.from(items)
        .map((item) => item.webkitGetAsEntry())
        .filter((entry): entry is FileSystemEntry => entry !== null);
}
