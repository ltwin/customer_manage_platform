package planningmedia

import (
	"errors"
	"testing"
)

func TestRightsMatrixIsExhaustiveAndFailClosed(t *testing.T) {
	sources := []SourceClass{SourceOfficial, SourceAnimeScreenshot, SourceSettingBook, SourceFan, SourceUnknownWeb, SourcePhotographerOwned, SourceLicensed, SourceCustomerSupplied}
	bases := []RightsBasis{RightsCitationOrDisplay, RightsOwnershipAttested, RightsLicenseRecorded, RightsDisplayConsent}
	wantBasis := map[SourceClass]RightsBasis{
		SourceOfficial: RightsCitationOrDisplay, SourceAnimeScreenshot: RightsCitationOrDisplay,
		SourceSettingBook: RightsCitationOrDisplay, SourceFan: RightsCitationOrDisplay,
		SourceUnknownWeb: RightsCitationOrDisplay, SourcePhotographerOwned: RightsOwnershipAttested,
		SourceLicensed: RightsLicenseRecorded, SourceCustomerSupplied: RightsDisplayConsent,
	}
	for _, source := range sources {
		for _, basis := range bases {
			for _, grant := range []bool{false, true} {
				in := RightsDeclarationInput{SourceClass: source, RightsBasis: basis, LicenseGenerationReferenceGranted: grant}
				valid := basis == wantBasis[source] && (!grant || source == SourceLicensed)
				for _, purpose := range []Purpose{PurposeMoodboardDisplay, PurposeShotReferenceDisplay, PurposeGenerationReference} {
					err := ValidatePurpose(in, purpose)
					allowed := valid && (purpose != PurposeGenerationReference || source == SourcePhotographerOwned || source == SourceLicensed && grant)
					if (err == nil) != allowed {
						t.Errorf("source=%s basis=%s grant=%v purpose=%s allowed=%v err=%v", source, basis, grant, purpose, allowed, err)
					}
				}
			}
		}
	}
	if err := ValidatePurpose(RightsDeclarationInput{SourceClass: "future", RightsBasis: RightsCitationOrDisplay}, PurposeMoodboardDisplay); !errors.Is(err, ErrRightsCombinationInvalid) {
		t.Fatalf("unknown source must fail closed: %v", err)
	}
	if err := ValidatePurpose(RightsDeclarationInput{SourceClass: SourceOfficial, RightsBasis: RightsCitationOrDisplay}, "future"); !errors.Is(err, ErrPurposeNotPermitted) {
		t.Fatalf("unknown purpose must fail closed: %v", err)
	}
}

func TestPlanningObjectKeyIsTypedAndCanonical(t *testing.T) {
	key, err := planningObjectKey("acc_test", "asset-123", 2, RenditionDisplay)
	if err != nil || key != "planning/acc_test/assets/asset-123/g2/display" {
		t.Fatalf("key=%q err=%v", key, err)
	}
	for _, input := range []struct {
		account, asset string
		generation     int
		kind           RenditionKind
	}{{"../escape", "asset", 1, RenditionDisplay}, {"account", "asset/escape", 1, RenditionDisplay}, {"account", "asset", 0, RenditionDisplay}, {"account", "asset", 1, "future"}} {
		if _, err := planningObjectKey(input.account, input.asset, input.generation, input.kind); err == nil {
			t.Fatalf("invalid key input accepted: %+v", input)
		}
	}
}
