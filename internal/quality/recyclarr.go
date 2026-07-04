// Package quality generates Recyclarr configuration from plain-language
// answers, mapping them onto TRaSH-guide quality profiles (issue #60).
// Arrsenal deliberately does NOT reimplement the TRaSH custom-format
// database — Recyclarr consumes the guides' own data and stays current; our
// job is the friendly layer that picks the right profiles and runs the sync.
package quality

import (
	"fmt"
	"strings"
)

// Image is pinned to the major: the generated config speaks the v8 schema,
// so a future v9 must be a deliberate upgrade — not a surprise breakage (v8
// itself broke v7's includes). `arrsenal update` re-pulls it. Both consumers
// share this pin: the one-shot sync at wiring time and the scheduled compose
// service (issue #106).
const Image = "ghcr.io/recyclarr/recyclarr:8"

// Answers are the quality choices, in user language.
type Answers struct {
	// Resolution: "1080p" or "2160p".
	Resolution string
	// Source: "bluray-web" (the standard tier) or "remux" (quality-first,
	// much larger files). The distinction shapes Radarr's profile; Sonarr's
	// TRaSH profiles are WEB-based either way.
	Source string
	// Anime adds the dedicated anime profile alongside the main one.
	Anime bool
}

// Validate rejects unknown enum values with the accepted ones named.
func (a Answers) Validate() error {
	switch a.Resolution {
	case "1080p", "2160p":
	default:
		return fmt.Errorf("resolution %q: pick 1080p or 2160p", a.Resolution)
	}
	switch a.Source {
	case "bluray-web", "remux":
	default:
		return fmt.Errorf("source %q: pick bluray-web or remux", a.Source)
	}
	return nil
}

// Instance is one arr as Recyclarr should reach it: the container-name URL
// and the API key the wiring engine already read.
type Instance struct {
	BaseURL string
	APIKey  string
}

// profile is one TRaSH-guide quality profile, addressed the way Recyclarr v8
// does: by its trash_id, a stable identifier the guides never change. The
// name is the guide's display name — it becomes the profile's name in the
// arr, and a comment for humans reading the generated file.
//
// Recyclarr v8 dropped the v7 include-template mechanism (its registry is
// empty on the v8 branch — learned by a live sync failure); the official v8
// templates are standalone configs whose entire payload is a
// quality_definition plus these trash_id references. We render the same
// schema directly; the IDs below are copied from those templates.
type profile struct {
	TrashID string
	Name    string
}

func sonarrProfiles(a Answers) []profile {
	main := profile{"72dae194fc92bf828f32cde7744e51a1", "WEB-1080p"}
	if a.Resolution == "2160p" {
		main = profile{"d1498e7d189fbe6c7110ceaabb7473e6", "WEB-2160p"}
	}
	p := []profile{main}
	if a.Anime {
		p = append(p, profile{"20e0fc959f1f1704bed501f23bdae76f", "[Anime] Remux-1080p"})
	}
	return p
}

func radarrProfiles(a Answers) []profile {
	var main profile
	switch {
	case a.Source == "remux" && a.Resolution == "2160p":
		main = profile{"fd161a61e3ab826d3a22d53f935696dd", "Remux + WEB 2160p"}
	case a.Source == "remux":
		main = profile{"9ca12ea80aa55ef916e3751f4b874151", "Remux + WEB 1080p"}
	case a.Resolution == "2160p":
		main = profile{"64fb5f9858489bdac2af690e27c8f42f", "UHD Bluray + WEB"}
	default:
		main = profile{"d1d67249d3890e49bc12e275d989a7e9", "HD Bluray + WEB"}
	}
	p := []profile{main}
	if a.Anime {
		p = append(p, profile{"722b624f9af1e492284c4bc842153a38", "[Anime] Remux-1080p"})
	}
	return p
}

// cfGroup mirrors one ACTIVE custom_format_groups.add entry from the
// official templates. These matter: most formats a group carries are flagged
// default in the guides and sync without being named, but the select lists
// include non-default ones (BR-DISK (BTN), the x265 golden rules) that sync
// ONLY when listed — omitting the block silently diverges from the official
// templates, and reset_unmatched_scores then re-zeroes any manual fix on
// every convergent pass. Found by audit, verified against every template.
type cfGroup struct {
	TrashID string
	Name    string
	Select  []cfSelect
}

type cfSelect struct {
	TrashID string
	Name    string
}

// The anime templates carry no active groups, so anime adds nothing here;
// the main choice's groups apply to the instance as a whole.
func sonarrGroups(a Answers) []cfGroup {
	golden := cfGroup{"158188097a58d7687dee647e04af0da3", "[Optional] Golden Rule HD",
		[]cfSelect{{"47435ece6b99a0b477caf360e79ba0bb", "x265 (HD)"}}}
	if a.Resolution == "2160p" {
		golden = cfGroup{"e3f37512790f00d0e89e54fe5e790d1c", "[Optional] Golden Rule UHD",
			[]cfSelect{{"9b64dff695c2115facf1b6ea59c9bd07", "x265 (no HDR/DV)"}}}
	}
	return []cfGroup{
		golden,
		{"85fae4a2294965b75710ef2989c850eb", "[Streaming Services] HD/UHD boost", []cfSelect{
			{"218e93e5702f44a68ad9e3c6ba87d2f0", "HD Streaming Boost"},
			{"43b3cf48cb385cd3eac608ee6bca7f09", "UHD Streaming Boost"},
		}},
		{"59c3af66780d08332fdc64e68297098f", "[Unwanted] Unwanted Formats", []cfSelect{
			{"15a05bc7c1a36e2b57fd628f8977e2fc", "AV1"},
			{"32b367365729d530ca1c124a0b180c64", "Bad Dual Groups"},
			{"85c61753df5da1fb2aab6f2a47426b09", "BR-DISK"},
			{"6f808933a71bd9666531610cb8c059cc", "BR-DISK (BTN)"},
			{"fbcb31d8dabd2a319072b84fc0b7249c", "Extras"},
			{"9c11cd3f07101cdba90a2d81cf0e56b4", "LQ"},
			{"e2315f990da2e2cbfc9fa5b7a6fcfe48", "LQ (Release Title)"},
			{"23297a736ca77c0fc8e70f8edd7ee56c", "Upscaled"},
		}},
	}
}

func radarrGroups(a Answers) []cfGroup {
	golden := cfGroup{"f8bf8eab4617f12dfdbd16303d8da245", "[Optional] Golden Rule HD",
		[]cfSelect{{"dc98083864ea246d05a42df0d05f81cc", "x265 (HD)"}}}
	if a.Resolution == "2160p" {
		golden = cfGroup{"ff204bbcecdd487d1cefcefdbf0c278d", "[Optional] Golden Rule UHD",
			[]cfSelect{{"839bea857ed2c0a8e084f3cbdbd65ecb", "x265 (no HDR/DV)"}}}
	}
	return []cfGroup{
		golden,
		{"a3ac6af01d78e4f21fcb75f601ac96df", "[Unwanted] Unwanted Formats", []cfSelect{
			{"b8cd450cbfa689c0259a01d9e29ba3d6", "3D"},
			{"cae4ca30163749b891686f95532519bd", "AV1"},
			{"b6832f586342ef70d9c128d40c07b872", "Bad Dual Groups"},
			{"cc444569854e9de0b084ab2b8b1532b2", "Black and White Editions"},
			{"ed38b889b31be83fda192888e2286d83", "BR-DISK"},
			{"0a3f082873eb454bde444150b70253cc", "Extras"},
			{"e6886871085226c3da1830830146846c", "Generated Dynamic HDR"},
			{"90a6f9a284dff5103f6346090e6280c8", "LQ"},
			{"e204b80c87be9497a8a6eaff48f72905", "LQ (Release Title)"},
			{"712d74cd88bceb883ee32f773656b1f5", "Sing-Along Versions"},
			{"bfd8eb01832d646a0a89c4deb46f8564", "Upscaled"},
		}},
	}
}

// MainProfileNames returns the display names of the main profiles the
// answers select — the names the synced profiles carry inside the arrs.
// Consumers (Seerr's default profile) match against these.
func MainProfileNames(a Answers) (sonarr, radarr string) {
	return sonarrProfiles(a)[0].Name, radarrProfiles(a)[0].Name
}

// RecyclarrConfig renders recyclarr.yml for the given instances. Instances
// are optional (nil = app not selected); at least one is required. Output is
// deterministic; the API keys appear here by necessity — the file lands 0600
// under Arrsenal's appdata (the caller's job), like every other secret file.
func RecyclarrConfig(a Answers, sonarr, radarr *Instance) ([]byte, error) {
	if err := a.Validate(); err != nil {
		return nil, err
	}
	if sonarr == nil && radarr == nil {
		return nil, fmt.Errorf("nothing to configure: neither Sonarr nor Radarr is available")
	}

	var b strings.Builder
	b.WriteString("# GENERATED BY ARRSENAL — regenerated every run from your quality answers.\n")
	// quality_definition types: an instance takes exactly one, so the main
	// choice wins; the anime profile rides alongside on the same definition.
	writeInstance(&b, "sonarr", "series", sonarr, sonarrProfiles(a), sonarrGroups(a))
	writeInstance(&b, "radarr", "movie", radarr, radarrProfiles(a), radarrGroups(a))
	return []byte(b.String()), nil
}

func writeInstance(b *strings.Builder, kind, defType string, inst *Instance, profiles []profile, groups []cfGroup) {
	if inst == nil {
		return
	}
	fmt.Fprintf(b, "%s:\n", kind)
	fmt.Fprintf(b, "  %s:\n", kind)
	fmt.Fprintf(b, "    base_url: %s\n", inst.BaseURL)
	fmt.Fprintf(b, "    api_key: %s\n", inst.APIKey)
	b.WriteString("    quality_definition:\n")
	fmt.Fprintf(b, "      type: %s\n", defType)
	b.WriteString("    quality_profiles:\n")
	for _, p := range profiles {
		fmt.Fprintf(b, "      - trash_id: %s # %s\n", p.TrashID, p.Name)
		b.WriteString("        reset_unmatched_scores:\n")
		b.WriteString("          enabled: true\n")
	}
	b.WriteString("    custom_format_groups:\n")
	b.WriteString("      add:\n")
	for _, g := range groups {
		fmt.Fprintf(b, "        - trash_id: %s # %s\n", g.TrashID, g.Name)
		b.WriteString("          select:\n")
		for _, s := range g.Select {
			fmt.Fprintf(b, "            - %s # %s\n", s.TrashID, s.Name)
		}
	}
}
