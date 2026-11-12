package errs

// Legacy codes retain the previous error identifiers in context metadata.
const (
	LegacyCodeField               = "legacy_code"
	LegacyCollectionNotFound      = "COLLECTION_NOT_FOUND"
	LegacyConfigInvalid           = "CONFIG_INVALID"
	LegacyConfigNotFound          = "CONFIG_NOT_FOUND"
	LegacyConfigWriteError        = "CONFIG_WRITE_ERROR"
	LegacyCsvError                = "CSV_ERROR"
	LegacyDownloadFailed          = "DOWNLOAD_FAILED"
	LegacyFileNotFound            = "FILE_NOT_FOUND"
	LegacyFileReadError           = "FILE_READ_ERROR"
	LegacyGithubAPIError          = "GITHUB_API_ERROR"
	LegacyGithubRateLimit         = "GITHUB_RATE_LIMIT"
	LegacyInvalidArgs             = "INVALID_ARGS"
	LegacyJSONError               = "JSON_ERROR"
	LegacyLockError               = "LOCK_ERROR"
	LegacyManifestCorrupted       = "MANIFEST_CORRUPTED"
	LegacyManifestGenerationError = "MANIFEST_GENERATION_ERROR"
	LegacyManifestNotFound        = "MANIFEST_NOT_FOUND"
	LegacyManifestReadError       = "MANIFEST_READ_ERROR"
	LegacyManifestWriteError      = "MANIFEST_WRITE_ERROR"
	LegacySearchError             = "SEARCH_ERROR"
	LegacySourceExists            = "SOURCE_EXISTS"
	LegacySourceNotFound          = "SOURCE_NOT_FOUND"
	LegacyUnknownSourceType       = "UNKNOWN_SOURCE_TYPE"
	LegacyWriteFailed             = "WRITE_FAILED"
)
