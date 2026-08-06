package planningmedia

import "errors"

type SourceClass string

const (
	SourceOfficial          SourceClass = "official"
	SourceAnimeScreenshot   SourceClass = "anime_screenshot"
	SourceSettingBook       SourceClass = "setting_book"
	SourceFan               SourceClass = "fan"
	SourceUnknownWeb        SourceClass = "unknown_web"
	SourcePhotographerOwned SourceClass = "photographer_owned"
	SourceLicensed          SourceClass = "licensed"
	SourceCustomerSupplied  SourceClass = "customer_supplied"
)

type RightsBasis string

const (
	RightsCitationOrDisplay RightsBasis = "citation_or_display"
	RightsOwnershipAttested RightsBasis = "ownership_attested"
	RightsLicenseRecorded   RightsBasis = "license_recorded"
	RightsDisplayConsent    RightsBasis = "display_consent"
)

type Purpose string

const (
	PurposeMoodboardDisplay     Purpose = "moodboard_display"
	PurposeShotReferenceDisplay Purpose = "shot_reference_display"
	PurposeGenerationReference  Purpose = "generation_reference"
)

type HolderKind string

const (
	HolderPlan HolderKind = "plan"
	HolderShot HolderKind = "shot"
)

type HolderMutation string

const (
	MutationUpload  HolderMutation = "upload"
	MutationBind    HolderMutation = "bind"
	MutationRelease HolderMutation = "release"
	MutationRead    HolderMutation = "read"
)

var (
	ErrRightsCombinationInvalid = errors.New("rights_combination_invalid")
	ErrPurposeNotPermitted      = errors.New("purpose_not_permitted")
	ErrGenerationGrantRequired  = errors.New("generation_reference_grant_required")
)

const MatrixVersion = 1

type RightsDeclarationInput struct {
	SourceClass                       SourceClass `json:"source_class"`
	RightsBasis                       RightsBasis `json:"rights_basis"`
	EvidenceSummary                   string      `json:"evidence_summary"`
	LicenseGenerationReferenceGranted bool        `json:"license_generation_reference_granted"`
}

func ValidateRights(in RightsDeclarationInput) error {
	if len([]rune(in.EvidenceSummary)) > 500 {
		return ErrRightsCombinationInvalid
	}
	want := map[SourceClass]RightsBasis{
		SourceOfficial: RightsCitationOrDisplay, SourceAnimeScreenshot: RightsCitationOrDisplay,
		SourceSettingBook: RightsCitationOrDisplay, SourceFan: RightsCitationOrDisplay,
		SourceUnknownWeb: RightsCitationOrDisplay, SourcePhotographerOwned: RightsOwnershipAttested,
		SourceLicensed: RightsLicenseRecorded, SourceCustomerSupplied: RightsDisplayConsent,
	}
	if want[in.SourceClass] == "" || want[in.SourceClass] != in.RightsBasis {
		return ErrRightsCombinationInvalid
	}
	if in.SourceClass != SourceLicensed && in.LicenseGenerationReferenceGranted {
		return ErrRightsCombinationInvalid
	}
	return nil
}

func PurposeAllowed(in RightsDeclarationInput, purpose Purpose) bool {
	if purpose == PurposeMoodboardDisplay || purpose == PurposeShotReferenceDisplay {
		return true
	}
	return purpose == PurposeGenerationReference &&
		(in.SourceClass == SourcePhotographerOwned || (in.SourceClass == SourceLicensed && in.LicenseGenerationReferenceGranted))
}

func ValidatePurpose(in RightsDeclarationInput, purpose Purpose) error {
	if err := ValidateRights(in); err != nil {
		return err
	}
	if !PurposeAllowed(in, purpose) {
		if purpose == PurposeGenerationReference {
			return ErrGenerationGrantRequired
		}
		return ErrPurposeNotPermitted
	}
	return nil
}
