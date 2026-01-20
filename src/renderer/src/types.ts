export type SortType =
    | "NAME:ASC"
    | "NAME:DESC"
    | "SIZE:ASC"
    | "SIZE:DESC"
    | "DATE:ASC"
    | "DATE:DESC";
export type LayoutType = "list" | "grid";

export interface ContentPreview {
    img?: {
        default: string;
        cover: string | null;
        thumbnail: string | null;
    };
    video?: {
        default: string;
    };
}
