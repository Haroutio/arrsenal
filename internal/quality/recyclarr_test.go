package quality

import (
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
)

func inst(url string) *Instance { return &Instance{BaseURL: url, APIKey: "key-" + url} }

type parsedInstance struct {
	BaseURL           string `yaml:"base_url"`
	APIKey            string `yaml:"api_key"`
	QualityDefinition struct {
		Type string `yaml:"type"`
	} `yaml:"quality_definition"`
	QualityProfiles []struct {
		TrashID              string `yaml:"trash_id"`
		ResetUnmatchedScores struct {
			Enabled bool `yaml:"enabled"`
		} `yaml:"reset_unmatched_scores"`
	} `yaml:"quality_profiles"`
}

func instanceFrom(t *testing.T, out []byte, kind string) *parsedInstance {
	t.Helper()
	var doc map[string]map[string]parsedInstance
	if err := yaml.Unmarshal(out, &doc); err != nil {
		t.Fatalf("generated config must parse: %v\n%s", err, out)
	}
	instances, ok := doc[kind]
	if !ok {
		return nil
	}
	i, ok := instances[kind]
	if !ok {
		t.Fatalf("%s block must contain an instance named %s:\n%s", kind, kind, out)
	}
	return &i
}

func profileIDs(i *parsedInstance) []string {
	var ids []string
	for _, p := range i.QualityProfiles {
		ids = append(ids, p.TrashID)
	}
	return ids
}

// IDs copied from the official v8 templates — the same source as the
// implementation, but spelled twice on purpose: a typo in either place
// breaks the match.
const (
	sonarrWEB1080p   = "72dae194fc92bf828f32cde7744e51a1"
	sonarrWEB2160p   = "d1498e7d189fbe6c7110ceaabb7473e6"
	sonarrAnime      = "20e0fc959f1f1704bed501f23bdae76f"
	radarrHDBluray   = "d1d67249d3890e49bc12e275d989a7e9"
	radarrUHD        = "64fb5f9858489bdac2af690e27c8f42f"
	radarrRemux1080p = "9ca12ea80aa55ef916e3751f4b874151"
	radarrRemux2160p = "fd161a61e3ab826d3a22d53f935696dd"
	radarrAnime      = "722b624f9af1e492284c4bc842153a38"
)

func TestProfileMapping(t *testing.T) {
	withAnime := func(main, anime string, on bool) []string {
		if on {
			return []string{main, anime}
		}
		return []string{main}
	}
	cases := []struct {
		a          Answers
		wantSonarr []string
		wantRadarr []string
	}{
		{Answers{Resolution: "1080p", Source: "bluray-web"},
			[]string{sonarrWEB1080p}, []string{radarrHDBluray}},
		{Answers{Resolution: "2160p", Source: "bluray-web"},
			[]string{sonarrWEB2160p}, []string{radarrUHD}},
		{Answers{Resolution: "1080p", Source: "remux"},
			[]string{sonarrWEB1080p}, []string{radarrRemux1080p}},
		{Answers{Resolution: "2160p", Source: "remux"},
			[]string{sonarrWEB2160p}, []string{radarrRemux2160p}},
		{Answers{Resolution: "1080p", Source: "bluray-web", Anime: true},
			withAnime(sonarrWEB1080p, sonarrAnime, true), withAnime(radarrHDBluray, radarrAnime, true)},
	}
	for _, tc := range cases {
		out, err := RecyclarrConfig(tc.a, inst("http://sonarr:8989"), inst("http://radarr:7878"))
		if err != nil {
			t.Fatalf("%+v: %v", tc.a, err)
		}
		gotS := profileIDs(instanceFrom(t, out, "sonarr"))
		gotR := profileIDs(instanceFrom(t, out, "radarr"))
		if strings.Join(gotS, ",") != strings.Join(tc.wantSonarr, ",") {
			t.Errorf("%+v sonarr = %v, want %v", tc.a, gotS, tc.wantSonarr)
		}
		if strings.Join(gotR, ",") != strings.Join(tc.wantRadarr, ",") {
			t.Errorf("%+v radarr = %v, want %v", tc.a, gotR, tc.wantRadarr)
		}
	}
}

func TestInstanceShape(t *testing.T) {
	a := Answers{Resolution: "1080p", Source: "bluray-web", Anime: true}
	out, err := RecyclarrConfig(a, inst("http://sonarr:8989"), inst("http://radarr:7878"))
	if err != nil {
		t.Fatal(err)
	}
	s := instanceFrom(t, out, "sonarr")
	r := instanceFrom(t, out, "radarr")
	if s.QualityDefinition.Type != "series" || r.QualityDefinition.Type != "movie" {
		t.Fatalf("quality_definition types = %q/%q, want series/movie",
			s.QualityDefinition.Type, r.QualityDefinition.Type)
	}
	for _, p := range append(s.QualityProfiles, r.QualityProfiles...) {
		if !p.ResetUnmatchedScores.Enabled {
			t.Fatalf("profile %s must reset unmatched scores (the official templates do)", p.TrashID)
		}
	}
}

func TestInstanceOmissionAndConnection(t *testing.T) {
	a := Answers{Resolution: "1080p", Source: "bluray-web"}

	out, err := RecyclarrConfig(a, inst("http://sonarr:8989"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "radarr:") {
		t.Fatal("unselected radarr must be absent")
	}
	got := instanceFrom(t, out, "sonarr")
	if got.BaseURL != "http://sonarr:8989" || got.APIKey != "key-http://sonarr:8989" {
		t.Fatalf("connection details:\n%s", out)
	}

	if _, err := RecyclarrConfig(a, nil, nil); err == nil {
		t.Fatal("no instances must be an error")
	}
}

func TestAnswerValidation(t *testing.T) {
	for _, bad := range []Answers{
		{Resolution: "720p", Source: "bluray-web"},
		{Resolution: "1080p", Source: "vhs"},
		{},
	} {
		if _, err := RecyclarrConfig(bad, inst("x"), nil); err == nil {
			t.Errorf("%+v must be rejected", bad)
		}
	}
}

func TestDeterministic(t *testing.T) {
	a := Answers{Resolution: "2160p", Source: "remux", Anime: true}
	first, err := RecyclarrConfig(a, inst("http://sonarr:8989"), inst("http://radarr:7878"))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		again, _ := RecyclarrConfig(a, inst("http://sonarr:8989"), inst("http://radarr:7878"))
		if string(again) != string(first) {
			t.Fatal("output must be deterministic")
		}
	}
}
