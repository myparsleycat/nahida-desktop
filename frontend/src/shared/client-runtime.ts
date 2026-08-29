import type { Treaty } from "@elysiajs/eden";
import { eden } from "@main/client";

export function getAkashaModIdGet() {
    return eden.akasha.content({ id: "" }).get;
}

export type ModIdGetResp = Treaty.Data<ReturnType<typeof getAkashaModIdGet>>;
