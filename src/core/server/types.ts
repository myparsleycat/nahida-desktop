import { t } from 'elysia';

export const imageDTO = t.Array(t.Object({
    filename: t.Nullable(t.String()),
    md5: t.Nullable(t.String()),
    size: t.Nullable(t.Number()),
    height: t.Nullable(t.Number()),
    width: t.Nullable(t.Number()),
    ext: t.Nullable(t.String()),
    created_at: t.Nullable(t.Number()),
    url: t.String()
}))

export const modderDTO = t.Object({
    id: t.String()
})



export const modDTO = t.Object({
    uuid: t.String(),
    modder: t.Nullable(modderDTO),
    version: t.String(),
    password: t.Boolean(),
    game: t.Union([
        t.Literal('genshin'),
        t.Literal('starrail'),
        t.Literal('wuwa'),
        t.Literal('zzz'),
        t.Literal('hk3rd')
    ]),
    title: t.String(),
    description: t.Nullable(t.String()),
    tags: t.Array(t.String()),
    imgs: imageDTO,
    dl_count: t.Number(),
    merged: t.Boolean(),
    swapkey: t.Nullable(t.Unknown()),
    preview_url: t.String(),
    arca_url: t.Nullable(t.String()),
    virustotal_url: t.Nullable(t.String()),
    vt_data: t.Nullable(t.Object({
        malicious: t.Number(),
        suspicious: t.Number(),
        undetected: t.Number(),
        harmless: t.Number(),
        timeout: t.Number(),
        'confirmed-timeout': t.Number(),
        failure: t.Number(),
        'type-unsupported': t.Number()
    })),
    sha256: t.String(),
    size: t.Number(),
    unzip_size: t.Number(),
    uploaded_at: t.Number(),
    expires_at: t.Nullable(t.Number()),
    expired: t.Boolean(),
    status: t.String(),
    vv: t.Number(),
    c_status: t.Object({
        expires_at: t.Nullable(t.Number()),
        is_active: t.Boolean(),
        is_deleted: t.Optional(t.Boolean())
    })
})