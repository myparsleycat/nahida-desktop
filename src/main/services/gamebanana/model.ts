import { z } from "zod";

const UrlString = z.string();
const HttpUrlString = z.url({ protocol: /^https?$/ });
const NumericId = z.coerce.number();

const PreviewImageSchema = z.object({
    _sType: z.string(),
    _sUrl: UrlString.optional(),
    _sBaseUrl: UrlString.optional(),
    _sCaption: z.string().optional(),
    _sFile: z.string().optional(),
    _sFile100: z.string().optional(),
    _hFile100: z.number().optional(),
    _wFile100: z.number().optional(),
    _sFile220: z.string().optional(),
    _hFile220: z.number().optional(),
    _wFile220: z.number().optional(),
    _sFile530: z.string().optional(),
    _hFile530: z.number().optional(),
    _wFile530: z.number().optional(),
    _sFile800: z.string().optional(),
    _hFile800: z.number().optional(),
    _wFile800: z.number().optional(),
});

const PreviewMediaSchema = z.preprocess(
    (val) => (Array.isArray(val) ? { _aImages: val } : val),
    z
        .object({
            _aMetadata: z.record(z.string(), z.unknown()).optional(),
            _aImages: z.array(PreviewImageSchema).optional(),
        })
        .catchall(z.unknown()),
);

const SubjectShaperSchema = z
    .object({
        _sBorderStyle: z.string(),
        _sFont: z.string(),
        _sTextColor: z.string(),
        _sTextHoverColor: z.string(),
        _sBorderColor: z.string().optional(),
        _sBorderHoverColor: z.string().optional(),
    })
    .catchall(z.unknown());

const MemberSchema = z
    .object({
        _idRow: NumericId,
        _sName: z.string(),
        _bIsOnline: z.boolean(),
        _bHasRipe: z.boolean(),
        _sProfileUrl: HttpUrlString,
        _sAvatarUrl: UrlString,
        _sMoreByUrl: UrlString.optional(),
        _sHdAvatarUrl: UrlString.optional(),
        _sUpicUrl: UrlString.optional(),
        _sHovatarUrl: UrlString.optional(),
        _sUserTitle: z.string().optional(),
        _sHonoraryTitle: z.string().optional(),
        _tsJoinDate: z.number().optional(),
        _sSigUrl: UrlString.optional(),
        _sPointsUrl: UrlString.optional(),
        _sMedalsUrl: UrlString.optional(),
        _sLocation: z.string().optional(),
        _sOnlineTitle: z.string().optional(),
        _sOfflineTitle: z.string().optional(),
        _nPoints: z.number().optional(),
        _nPointsRank: z.number().optional(),
        _nBuddyCount: z.number().optional(),
        _nSubscriberCount: z.number().optional(),
        _aClearanceLevels: z.array(z.string()).optional(),
        _aSubjectShaper: SubjectShaperSchema.optional(),
        _sSubjectShaperCssCode: z.string().optional(),
        _aNormalMedals: z.array(z.array(z.union([z.string(), z.number()]))).optional(),
        _aRareMedals: z.array(z.array(z.union([z.string(), z.number()]))).optional(),
        _aLegendaryMedals: z.array(z.array(z.union([z.string(), z.number()]))).optional(),
        _aDonationMethods: z.array(z.unknown()).optional(),
        _aDefaultLicenseChecklist: z
            .union([z.array(z.string()), z.record(z.string(), z.unknown())])
            .optional(),
        _sDefaultLicense: z.string().optional(),
        _bAccessorIsBuddy: z.boolean().optional(),
        _bBuddyRequestExistsWithAccessor: z.boolean().optional(),
        _bAccessorIsSubscribed: z.boolean().optional(),
    })
    .catchall(z.unknown());

const BasicCategorySchema = z
    .object({
        _idRow: z.number().optional(),
        _sName: z.string(),
        _sModelName: z.string().optional(),
        _sProfileUrl: UrlString.optional(),
        _sUrl: UrlString.optional(),
        _sIconUrl: UrlString.optional(),
        _nItemCount: z.number().optional(),
        _nCategoryCount: z.number().optional(),
        _bIsObsolete: z.boolean().optional(),
    })
    .catchall(z.unknown());

const GameSchema = z
    .object({
        _idRow: NumericId,
        _sName: z.string(),
        _sAbbreviation: z.string().optional(),
        _sProfileUrl: HttpUrlString,
        _sIconUrl: UrlString,
        _sBannerUrl: UrlString.optional(),
        _nSubscriberCount: z.number().optional(),
        _bHasSubmissionQueue: z.boolean().optional(),
        _bAccessorIsSubscribed: z.boolean().optional(),
        _idAccessorSubscriptionRow: z.number().nullable().optional(),
    })
    .catchall(z.unknown());

const SubmissionRecordSchema = z
    .object({
        _idRow: NumericId,
        _sModelName: z.string(),
        _sSingularTitle: z.string().optional(),
        _sIconClasses: z.string().optional(),
        _sName: z.string(),
        _sProfileUrl: HttpUrlString,
        _tsDateAdded: z.number().optional(),
        _tsDateModified: z.number().optional(),
        _tsDateUpdated: z.number().optional(),
        _bHasFiles: z.boolean().optional(),
        _aTags: z.array(z.union([z.string(), z.unknown()])).optional(),
        _aPreviewMedia: PreviewMediaSchema.optional(),
        _aSubmitter: MemberSchema,
        _aGame: GameSchema.optional(),
        _aRootCategory: BasicCategorySchema.optional(),
        _aSubCategory: BasicCategorySchema.optional(),
        _sVersion: z.string().optional(),
        _sDescription: z.string().optional(),
        _sPeriod: z.string().optional(),
        _sInitialVisibility: z.string(),
        _bHasContentRatings: z.boolean().optional(),
        _bIsObsolete: z.boolean().optional(),
        _bWasFeatured: z.boolean().optional(),
        _bIsOwnedByAccessor: z.boolean().optional(),
        _nLikeCount: z.number().optional(),
        _nPostCount: z.number().optional(),
        _nViewCount: z.number().optional(),
    })
    .catchall(z.unknown());

const FeedSchema = z.object({
    _aMetadata: z.object({
        _nRecordCount: z.number(),
        _nPerpage: z.number(),
        _bIsComplete: z.boolean(),
    }),
    _aRecords: z.array(SubmissionRecordSchema),
});

const GameManagerSchema = z.object({
    _sLevel: z.string(),
    _tsDateAdded: z.number(),
    _aMember: MemberSchema,
});

const GameSectionSchema = z.object({
    _sModelName: z.string(),
    _sPluralTitle: z.string(),
    _nItemCount: z.number(),
    _nCategoryCount: z.number(),
    _sUrl: z.string(),
});

export const GameProfileSchema = z
    .object({
        _idRow: NumericId,
        _nStatus: z.string(),
        _bIsPrivate: z.boolean(),
        _bAccessorIsSubmitter: z.boolean(),
        _bIsTrashed: z.boolean(),
        _bIsWithheld: z.boolean(),
        _sName: z.string(),
        _tsDateModified: z.number(),
        _tsDateAdded: z.number(),
        _sProfileUrl: HttpUrlString,
        _aPreviewMedia: PreviewMediaSchema,
        _sInitialVisibility: z.string(),
        _bHasFiles: z.boolean(),
        _nSubscriberCount: z.number(),
        _sAbbreviation: z.string().optional(),
        _sHomepage: z.string().optional(),
        _dsReleaseDate: z.string().optional(),
        _bShowRipePromo: z.boolean(),
        _bFollowLinks: z.boolean(),
        _idAccessorSubscriptionRow: z.number().nullable().optional(),
        _bAccessorIsSubscribed: z.boolean(),
        _sWelcomeMessage: z.string().optional(),
        _aManagers: z.array(GameManagerSchema),
        _aSections: z.array(GameSectionSchema),
        _aModRootCategories: z.array(BasicCategorySchema),
        _bIsApproved: z.boolean(),
    })
    .catchall(z.unknown());

export const GameTopSubsSchema = z.array(SubmissionRecordSchema);
export const GameSubfeedSchema = FeedSchema;

export const ModIndexSchema = FeedSchema;

export const ModCategoryProfileSchema = z
    .object({
        _idRow: NumericId,
        _tsDateModified: z.number(),
        _tsDateAdded: z.number(),
        _sName: z.string(),
        _sProfileUrl: UrlString.optional(),
        _sText: z.string(),
        _bIsObsolete: z.boolean(),
        _sIconUrl: UrlString.optional(),
        _aSubmitter: MemberSchema,
        _bIsTrashed: z.boolean(),
        _aGame: GameSchema,
    })
    .catchall(z.unknown());

export const ModCategoriesSchema = z.array(
    z
        .object({
            _idRow: NumericId,
            _sName: z.string(),
            _nItemCount: z.number(),
            _nCategoryCount: z.number(),
            _sUrl: UrlString,
            _bIsObsolete: z.boolean(),
            _sIconUrl: UrlString.optional(),
        })
        .catchall(z.unknown()),
);

const ModFileSchema = z
    .object({
        _idRow: NumericId,
        _sFile: z.string(),
        _nFilesize: z.number(),
        _tsDateAdded: z.number(),
        _nDownloadCount: z.number(),
        _sDownloadUrl: HttpUrlString,
        _sMd5Checksum: z.string().optional(),
        _sAnalysisState: z.string().optional(),
        _sAnalysisResult: z.string().optional(),
        _sAnalysisResultVerbose: z.string().optional(),
        _sAvState: z.string().optional(),
        _sAvResult: z.string().optional(),
        _bIsArchived: z.boolean().optional(),
        _bHasContents: z.boolean().optional(),
    })
    .catchall(z.unknown());

const ModLicenseChecklistSchema = z.object({
    yes: z.array(z.string()).optional().default([]),
    ask: z.array(z.string()).optional().default([]),
    no: z.array(z.string()).optional().default([]),
});

const ModCreditsAuthorSchema = z
    .object({
        _sRole: z.string().optional(),
        _idRow: NumericId.optional(),
        _sName: z.string(),
        _sUpicUrl: UrlString.optional(),
        _sProfileUrl: HttpUrlString.optional(),
        _sAvatarUrl: UrlString.optional(),
        _bIsOnline: z.boolean().optional(),
    })
    .catchall(z.unknown());

const ModCreditsGroupSchema = z.object({
    _sGroupName: z.string(),
    _aAuthors: z.array(ModCreditsAuthorSchema),
});

const ModPostStampSchema = z
    .object({
        _sTitle: z.string(),
        _sIconClasses: z.string().optional(),
        _sCategory: z.string().optional(),
        _nCount: z.number().optional(),
    })
    .catchall(z.unknown());

const ModPostAccessSchema = z.record(z.string(), z.boolean());

const ModPostRecordSchema = z
    .object({
        _idRow: NumericId,
        _nStatus: z.string(),
        _tsDateAdded: z.number().optional(),
        _tsDateModified: z.number().optional(),
        _nReplyCount: z.number().optional(),
        _iPinLevel: z.number().optional(),
        _nStampScore: z.number().optional(),
        _aPreviewMedia: PreviewMediaSchema.optional(),
        _sText: z.string(),
        _aPoster: MemberSchema.optional(),
        _bFollowLinks: z.boolean().optional(),
        _aStamps: z.array(ModPostStampSchema).optional(),
        _aAccess: ModPostAccessSchema.optional(),
    })
    .catchall(z.unknown());

export const ModProfileSchema = z
    .object({
        _idRow: NumericId,
        _nStatus: z.string(),
        _bIsPrivate: z.boolean(),
        _bAccessorIsSubmitter: z.boolean(),
        _bIsTrashed: z.boolean(),
        _bIsWithheld: z.boolean(),
        _sName: z.string(),
        _sCommentsMode: z.string(),
        _tsDateModified: z.number(),
        _tsDateAdded: z.number(),
        _sProfileUrl: HttpUrlString,
        _aPreviewMedia: PreviewMediaSchema,
        _nUpdatesCount: z.number(),
        _bHasUpdates: z.boolean(),
        _nAllTodosCount: z.number(),
        _bHasTodos: z.boolean(),
        _nPostCount: z.number(),
        _aTags: z.array(z.union([z.string(), z.record(z.string(), z.unknown())])),
        _bCreatedBySubmitter: z.boolean(),
        _bIsPorted: z.boolean(),
        _nThanksCount: z.number(),
        _sInitialVisibility: z.string(),
        _sDownloadUrl: HttpUrlString,
        _nDownloadCount: z.number(),
        _aFiles: z.array(ModFileSchema),
        _nSubscriberCount: z.number(),
        _aContributingStudios: z.array(z.unknown()),
        _sLicense: z.string(),
        _aLicenseChecklist: ModLicenseChecklistSchema,
        _sDescription: z.string().optional(),
        _bGenerateTableOfContents: z.boolean(),
        _sText: z.string(),
        _bIsObsolete: z.boolean(),
        _nLikeCount: z.number(),
        _bAccessorHasUnliked: z.boolean(),
        _bAccessorHasLiked: z.boolean(),
        _nViewCount: z.number(),
        _sVersion: z.string(),
        _bAcceptsDonations: z.boolean(),
        _bShowRipePromo: z.boolean(),
        _aEmbeddables: z
            .object({
                _sEmbeddableImageBaseUrl: UrlString,
                _aVariants: z.array(z.string()),
            })
            .catchall(z.unknown()),
        _aSubmitter: MemberSchema,
        _bFollowLinks: z.boolean(),
        _aGame: GameSchema,
        _aCategory: BasicCategorySchema,
        _aSuperCategory: BasicCategorySchema.optional(),
        _aCredits: z.array(ModCreditsGroupSchema),
        _idAccessorSubscriptionRow: z.number().nullable().optional(),
        _bAccessorIsSubscribed: z.boolean(),
        _bAccessorHasThanked: z.boolean(),
    })
    .catchall(z.unknown());

export const ModConfigSchema = z.object({
    _aAccess: z.record(z.string(), z.boolean()),
    _bAccessorIsSubmitter: z.boolean(),
});

export const ModPostsSchema = z.object({
    _aMetadata: z.object({
        _nRecordCount: z.number(),
        _nPerpage: z.number(),
        _bIsComplete: z.boolean(),
    }),
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
