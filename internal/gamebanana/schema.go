package gamebanana

import (
	"errors"
	"fmt"
	"math"
	"net/url"
	"slices"
	"strconv"
	"strings"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

type responseSchema struct {
	context string
	rule    validation.Rule
}

type schemaField struct {
	name     string
	required bool
	rules    []validation.Rule
}

var (
	stringSchema = validation.By(func(value any) error {
		if _, ok := value.(string); !ok {
			return errors.New("expected string")
		}
		return nil
	})
	boolSchema = validation.By(func(value any) error {
		if _, ok := value.(bool); !ok {
			return errors.New("expected boolean")
		}
		return nil
	})
	numberSchema    = validation.By(validateNumber)
	numericIDSchema = validation.By(func(value any) error {
		if err := validateNumber(value); err == nil {
			return nil
		}
		str, ok := value.(string)
		if !ok {
			return errors.New("expected number")
		}
		number, err := strconv.ParseFloat(strings.TrimSpace(str), 64)
		if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
			return errors.New("expected number")
		}
		return nil
	})
	httpURLSchema = validation.By(func(value any) error {
		str, ok := value.(string)
		if !ok {
			return errors.New("expected string")
		}
		parsed, err := url.Parse(str)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return errors.New("expected HTTP URL")
		}
		return nil
	})
	statusSchema = validation.By(func(value any) error {
		if _, ok := value.(string); ok {
			return nil
		}
		return validateNumber(value)
	})
)

var (
	previewImageSchema = objectSchema(
		optionalField("_sUrl", stringSchema),
		optionalField("_sBaseUrl", stringSchema),
		optionalField("_sCaption", stringSchema),
		optionalField("_sFile", stringSchema),
		optionalField("_sFile100", stringSchema),
		optionalField("_sFile220", stringSchema),
		optionalField("_sFile530", stringSchema),
	)
	previewMediaSchema = objectSchema(
		optionalField("screenshots", arraySchema(previewImageSchema)),
	)
	memberSchema = objectSchema(
		requiredField("_sName", stringSchema),
		optionalField("_sAvatarUrl", stringSchema),
		optionalField("_sProfileUrl", httpURLSchema),
	)
	nestedCategorySchema = objectSchema(
		optionalField("_idRow", numericIDSchema),
		requiredField("_sName", stringSchema),
		optionalField("_sIconUrl", stringSchema),
		optionalField("_nItemCount", numberSchema),
		optionalField("_nCategoryCount", numberSchema),
		optionalField("_sProfileUrl", stringSchema),
		optionalField("_sUrl", stringSchema),
	)
	basicCategorySchema = objectSchema(
		requiredField("_idRow", numericIDSchema),
		requiredField("_sName", stringSchema),
		optionalField("_sIconUrl", stringSchema),
		optionalField("_nItemCount", numberSchema),
		optionalField("_nCategoryCount", numberSchema),
		optionalField("_sProfileUrl", stringSchema),
		optionalField("_sUrl", stringSchema),
	)
	gameSchema = objectSchema(
		requiredField("_idRow", numericIDSchema),
		requiredField("_sName", stringSchema),
	)
	submissionRecordSchema = objectSchema(
		requiredField("_idRow", numericIDSchema),
		requiredField("_sModelName", stringSchema),
		requiredField("_sName", stringSchema),
		optionalField("_tsDateAdded", numberSchema),
		optionalField("_tsDateModified", numberSchema),
		optionalField("_tsDateUpdated", numberSchema),
		optionalField("_aPreviewContent", previewMediaSchema),
		requiredField("_aSubmitter", memberSchema),
		optionalField("_aRootCategory", nestedCategorySchema),
		optionalField("_aSubCategory", nestedCategorySchema),
		optionalField("_sDescription", stringSchema),
		optionalField("_nLikeCount", numberSchema),
		optionalField("_nPostCount", numberSchema),
		optionalField("_nViewCount", numberSchema),
	)
	feedMetadataSchema = objectSchema(
		requiredField("_nRecordCount", numberSchema),
		requiredField("_nPerpage", numberSchema),
		requiredField("_bIsComplete", boolSchema),
	)
	feedSchema = objectSchema(
		requiredField("_aMetadata", feedMetadataSchema),
		requiredField("_aRecords", arraySchema(submissionRecordSchema)),
	)
	gameProfileSchema = objectSchema(
		requiredField("_idRow", numericIDSchema),
		requiredField("_sName", stringSchema),
		requiredField("_sProfileUrl", httpURLSchema),
		requiredField("_aModRootCategories", arraySchema(basicCategorySchema)),
	)
	modCategoryProfileSchema = objectSchema(
		requiredField("_idRow", numericIDSchema),
		requiredField("_sName", stringSchema),
		optionalField("_sProfileUrl", stringSchema),
	)
	modFileSchema = objectSchema(
		requiredField("_idRow", numericIDSchema),
		requiredField("_sFile", stringSchema),
		requiredField("_tsDateAdded", numberSchema),
		requiredField("_nDownloadCount", numberSchema),
		requiredField("_sDownloadUrl", httpURLSchema),
		optionalField("_sMd5Checksum", stringSchema),
		optionalField("_sVersion", stringSchema),
		optionalField("_sDescription", stringSchema),
	)
	modPostStampSchema = objectSchema(
		optionalField("_nCount", numberSchema),
	)
	modPostRecordSchema = objectSchema(
		requiredField("_idRow", numericIDSchema),
		requiredField("_nStatus", statusSchema),
		optionalField("_tsDateAdded", numberSchema),
		optionalField("_nReplyCount", numberSchema),
		requiredField("_sText", stringSchema),
		optionalField("_aPoster", memberSchema),
		optionalField("_aStamps", arraySchema(modPostStampSchema)),
	)
	modProfileSchema = objectSchema(
		requiredField("_idRow", numericIDSchema),
		requiredField("_sName", stringSchema),
		requiredField("_sProfileUrl", httpURLSchema),
		optionalField("_aPreviewContent", previewMediaSchema),
		optionalField("_nPostCount", numberSchema),
		optionalField("_nDownloadCount", numberSchema),
		optionalField("_aFiles", arraySchema(modFileSchema)),
		optionalField("_sText", stringSchema),
		optionalField("_nLikeCount", numberSchema),
		optionalField("_nViewCount", numberSchema),
		optionalField("_bAccessorHasLiked", boolSchema),
		optionalField("_bAccessorHasUnliked", boolSchema),
		requiredField("_aSubmitter", memberSchema),
		requiredField("_aGame", gameSchema),
		requiredField("_aCategory", nestedCategorySchema),
	)
	modConfigSchema = objectSchema(
		optionalField("_aAccess", boolRecordSchema()),
		optionalField("_bAccessorIsSubmitter", boolSchema),
	)
	modPostsSchema = objectSchema(
		requiredField("_aMetadata", feedMetadataSchema),
		requiredField("_aRecords", arraySchema(modPostRecordSchema)),
	)
)

var (
	gameProfileResponseSchema        = responseSchema{"game_profile", gameProfileSchema}
	gameTopSubsResponseSchema        = responseSchema{"game_top_submissions", arraySchema(submissionRecordSchema)}
	gameSubfeedResponseSchema        = responseSchema{"game_subfeed", feedSchema}
	modIndexResponseSchema           = responseSchema{"mod_index", feedSchema}
	modCategoryProfileResponseSchema = responseSchema{"mod_category_profile", modCategoryProfileSchema}
	modCategoriesResponseSchema      = responseSchema{"mod_categories", arraySchema(basicCategorySchema)}
)

func modelResponseSchema(model, suffix string, rule validation.Rule) responseSchema {
	return responseSchema{context: strings.ToLower(model) + "_" + suffix, rule: rule}
}

func (s responseSchema) validate(value any) error {
	err := validation.Validate(value, s.rule)
	if err == nil {
		return nil
	}
	issues := make([]string, 0, 3)
	collectSchemaIssues("", err, &issues)
	return errors.New("GAMEBANANA_SCHEMA_ERROR:" + s.context + ":" + strings.Join(issues, " | "))
}

func requiredField(name string, rules ...validation.Rule) schemaField {
	return schemaField{name: name, required: true, rules: rules}
}

func optionalField(name string, rules ...validation.Rule) schemaField {
	return schemaField{name: name, rules: rules}
}

func objectSchema(fields ...schemaField) validation.Rule {
	return validation.By(func(value any) error {
		record, ok := value.(map[string]any)
		if !ok {
			return errors.New("expected object")
		}
		errs := validation.Errors{}
		for _, field := range fields {
			fieldValue, exists := record[field.name]
			if !exists {
				if field.required {
					errs[field.name] = errors.New("is required")
				}
				continue
			}
			if err := validation.Validate(fieldValue, field.rules...); err != nil {
				errs[field.name] = err
			}
		}
		return errs.Filter()
	})
}

func arraySchema(item validation.Rule) validation.Rule {
	return validation.By(func(value any) error {
		if _, ok := value.([]any); !ok {
			return errors.New("expected array")
		}
		return validation.Validate(value, validation.Each(item))
	})
}

func boolRecordSchema() validation.Rule {
	return validation.By(func(value any) error {
		if _, ok := value.(map[string]any); !ok {
			return errors.New("expected object")
		}
		return validation.Validate(value, validation.Each(boolSchema))
	})
}

func validateNumber(value any) error {
	var number float64
	switch typed := value.(type) {
	case float64:
		number = typed
	case float32:
		number = float64(typed)
	case int:
		number = float64(typed)
	case int8:
		number = float64(typed)
	case int16:
		number = float64(typed)
	case int32:
		number = float64(typed)
	case int64:
		number = float64(typed)
	case uint:
		number = float64(typed)
	case uint8:
		number = float64(typed)
	case uint16:
		number = float64(typed)
	case uint32:
		number = float64(typed)
	case uint64:
		number = float64(typed)
	default:
		return errors.New("expected number")
	}
	if math.IsNaN(number) || math.IsInf(number, 0) {
		return errors.New("expected finite number")
	}
	return nil
}

func collectSchemaIssues(path string, err error, issues *[]string) {
	if len(*issues) == 3 {
		return
	}
	var nested validation.Errors
	if errors.As(err, &nested) {
		keys := make([]string, 0, len(nested))
		for key := range nested {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		for _, key := range keys {
			next := key
			if path != "" {
				next = path + "." + key
			}
			collectSchemaIssues(next, nested[key], issues)
			if len(*issues) == 3 {
				return
			}
		}
		return
	}
	if path == "" {
		path = "root"
	}
	*issues = append(*issues, fmt.Sprintf("%s: %s", path, err))
}
