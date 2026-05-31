import { z } from "zod";

const UrlString = z.string();
const HttpUrlString = z.url({ protocol: /^https?$/ });
const NumericId = z.coerce.number();
const OptionalNumericId = z.preprocess((value) => {
    if (value === null || value === undefined || value === "") {
        return undefined;
    }

    const numberValue =
        typeof value === "number"
            ? value
            : Number(typeof value === "string" ? value.trim() : value);

    return Number.isFinite(numberValue) ? numberValue : undefined;
}, z.number().optional());
const StatusValue = z.union([z.string(), z.number()]).transform((value) => String(value));

const PreviewImageSchema = z
    .object({
        _sUrl: UrlString.optional(),
        _sBaseUrl: UrlString.optional(),
        _sCaption: z.string().optional(),
        _sFile: z.string().optional(),
        _sFile530: z.string().optional(),
        _sFile800: z.string().optional(),
    })
    .catchall(z.unknown());

const PreviewMediaSchema = z.preprocess(
    (value) => (Array.isArray(value) ? { _aImages: value } : value),
    z
        .object({
            _aImages: z.array(PreviewImageSchema).optional(),
        })
        .catchall(z.unknown()),
);

const MemberSchema = z
    .object({
        _sName: z.string(),
        _sAvatarUrl: UrlString.optional(),
        _sProfileUrl: HttpUrlString.optional(),
    })
    .catchall(z.unknown());

const NestedCategorySchema = z
    .object({
        _idRow: OptionalNumericId,
        _sName: z.string(),
        _sIconUrl: UrlString.optional(),
        _nItemCount: z.number().optional(),
        _nCategoryCount: z.number().optional(),
        _sProfileUrl: UrlString.optional(),
        _sUrl: UrlString.optional(),
    })
    .catchall(z.unknown());

const BasicCategorySchema = z
    .object({
        _idRow: NumericId,
        _sName: z.string(),
        _sIconUrl: UrlString.optional(),
        _nItemCount: z.number().optional(),
        _nCategoryCount: z.number().optional(),
        _sProfileUrl: UrlString.optional(),
        _sUrl: UrlString.optional(),
    })
    .catchall(z.unknown());

const GameSchema = z
    .object({
        _sName: z.string(),
    })
    .catchall(z.unknown());

const SubmissionRecordSchema = z
    .object({
        _idRow: NumericId,
        _sModelName: z.string(),
        _sName: z.string(),
        _tsDateAdded: z.number().optional(),
        _tsDateModified: z.number().optional(),
        _tsDateUpdated: z.number().optional(),
        _aPreviewMedia: PreviewMediaSchema.optional(),
        _aSubmitter: MemberSchema,
        _aRootCategory: NestedCategorySchema.optional(),
        _aSubCategory: NestedCategorySchema.optional(),
        _sDescription: z.string().optional(),
        _nLikeCount: z.number().optional(),
        _nPostCount: z.number().optional(),
        _nViewCount: z.number().optional(),
    })
    .catchall(z.unknown());

const FeedMetadataSchema = z.object({
    _nRecordCount: z.number(),
    _nPerpage: z.number(),
    _bIsComplete: z.boolean(),
});

const FeedSchema = z.object({
    _aMetadata: FeedMetadataSchema,
    _aRecords: z.array(SubmissionRecordSchema),
});

export const GameProfileSchema = z
    .object({
        _idRow: NumericId,
        _sName: z.string(),
        _sProfileUrl: HttpUrlString,
        _aModRootCategories: z.array(BasicCategorySchema),
    })
    .catchall(z.unknown());

export const GameTopSubsSchema = z.array(SubmissionRecordSchema);
export const GameSubfeedSchema = FeedSchema;

export const ModIndexSchema = FeedSchema;

export const ModCategoryProfileSchema = z
    .object({
        _idRow: NumericId,
        _sName: z.string(),
        _sProfileUrl: UrlString.optional(),
    })
    .catchall(z.unknown());

export const ModCategoriesSchema = z.array(BasicCategorySchema);

const ModFileSchema = z
    .object({
        _idRow: NumericId,
        _sFile: z.string(),
        _tsDateAdded: z.number(),
        _nDownloadCount: z.number(),
        _sDownloadUrl: HttpUrlString,
        _sMd5Checksum: z.string().optional(),
        _sVersion: z.string().optional(),
        _sDescription: z.string().optional(),
    })
    .catchall(z.unknown());

const ModPostStampSchema = z
    .object({
        _nCount: z.number().optional(),
    })
    .catchall(z.unknown());

const ModPostRecordSchema = z
    .object({
        _idRow: NumericId,
        _nStatus: StatusValue,
        _tsDateAdded: z.number().optional(),
        _nReplyCount: z.number().optional(),
        _sText: z.string(),
        _aPoster: MemberSchema.optional(),
        _aStamps: z.array(ModPostStampSchema).optional(),
    })
    .catchall(z.unknown());

export const ModProfileSchema = z
    .object({
        _idRow: NumericId,
        _sName: z.string(),
        _sProfileUrl: HttpUrlString,
        _aPreviewMedia: PreviewMediaSchema.optional(),
        _nPostCount: z.number().optional(),
        _nDownloadCount: z.number().optional(),
        _aFiles: z.array(ModFileSchema).optional(),
        _sText: z.string().optional(),
        _nLikeCount: z.number().optional(),
        _nViewCount: z.number().optional(),
        _aSubmitter: MemberSchema,
        _aGame: GameSchema,
        _aCategory: NestedCategorySchema,
    })
    .catchall(z.unknown());

export const ModConfigSchema = z
    .object({
        _aAccess: z.record(z.string(), z.boolean()).optional(),
        _bAccessorIsSubmitter: z.boolean().optional(),
    })
    .catchall(z.unknown());

export const ModPostsSchema = z.object({
    _aMetadata: FeedMetadataSchema,
    _aRecords: z.array(ModPostRecordSchema),
});

export const MemberNavigatorPersonalSchema = z
    .object({
        _sProfileUrl: HttpUrlString,
        _sUsername: z.string(),
        _sAvatarUrl: UrlString,
    })
    .catchall(z.unknown());

export const GameBananaLoginRequiredSchema = z.object({
    _sErrorCode: z.literal("LOGIN_REQUIRED"),
    _sErrorMessage: z.string(),
});

export type GameProfile = z.infer<typeof GameProfileSchema>;
export type GameTopSubs = z.infer<typeof GameTopSubsSchema>;
export type GameSubfeed = z.infer<typeof GameSubfeedSchema>;
export type ModIndex = z.infer<typeof ModIndexSchema>;
export type ModCategoryProfile = z.infer<typeof ModCategoryProfileSchema>;
export type ModCategories = z.infer<typeof ModCategoriesSchema>;
export type ModProfile = z.infer<typeof ModProfileSchema>;
export type ModConfig = z.infer<typeof ModConfigSchema>;
export type ModPosts = z.infer<typeof ModPostsSchema>;
