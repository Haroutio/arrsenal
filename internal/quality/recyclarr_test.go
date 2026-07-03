package quality

import (
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
)

func inst(url string) *Instance { return &Instance{BaseURL: url, APIKey: "key-" + url} }

func templatesFrom(t *testing.T, out []byte, kind string) []string {
	t.Helper()
	var doc map[string]map[string]struct {
		BaseURL string `yaml:"base_url"`
		APIKey  string `yaml:"api_key"`
		Include []struct {
			Template string `yaml:"template"`
		} `yaml:"include"`
	}
	if err := yaml.Unmarshal(out, &doc); err != nil {
		t.Fatalf("generated config must parse: %v\n%s", err, out)
	}
	instances, ok := doc[kind]
	if !ok {
		return nil
	}
	var names []string
	for _, i := range instances {
		for _, inc := range i.Include {
			names = append(names, inc.Template)
		}
	}
	return names
}

func TestTemplateMapping(t *testing.T) {
	sonarrSet := func(res string, anime bool) []string {
		t := []string{
			"sonarr-quality-definition-series",
			"sonarr-v4-quality-profile-web-" + res,
			"sonarr-v4-custom-formats-web-" + res,
		}
		if anime {
			t = append(t, "sonarr-v4-quality-profile-anime", "sonarr-v4-custom-formats-anime")
		}
		return t
	}
	radarrSet := func(main string, anime bool) []string {
		t := []string{
			"radarr-quality-definition-movie",
			"radarr-quality-profile-" + main,
			"radarr-custom-formats-" + main,
		}
		if anime {
			t = append(t, "radarr-quality-profile-anime", "radarr-custom-formats-anime")
		}
		return t
	}
	cases := []struct {
		a          Answers
		wantSonarr []string
		wantRadarr []string
	}{
		{Answers{Resolution: "1080p", Source: "bluray-web"},
			sonarrSet("1080p", false), radarrSet("hd-bluray-web", false)},
		{Answers{Resolution: "2160p", Source: "bluray-web"},
			sonarrSet("2160p", false), radarrSet("uhd-bluray-web", false)},
		{Answers{Resolution: "1080p", Source: "remux"},
			sonarrSet("1080p", false), radarrSet("remux-web-1080p", false)},
		{Answers{Resolution: "2160p", Source: "remux"},
			sonarrSet("2160p", false), radarrSet("remux-web-2160p", false)},
		{Answers{Resolution: "1080p", Source: "bluray-web", Anime: true},
			sonarrSet("1080p", true), radarrSet("hd-bluray-web", true)},
	}
	for _, tc := range cases {
		out, err := RecyclarrConfig(tc.a, inst("http://sonarr:8989"), inst("http://radarr:7878"))
		if err != nil {
			t.Fatalf("%+v: %v", tc.a, err)
		}
		gotS := templatesFrom(t, out, "sonarr")
		gotR := templatesFrom(t, out, "radarr")
		if strings.Join(gotS, ",") != strings.Join(tc.wantSonarr, ",") {
			t.Errorf("%+v sonarr = %v, want %v", tc.a, gotS, tc.wantSonarr)
		}
		if strings.Join(gotR, ",") != strings.Join(tc.wantRadarr, ",") {
			t.Errorf("%+v radarr = %v, want %v", tc.a, gotR, tc.wantRadarr)
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
	if !strings.Contains(string(out), "base_url: http://sonarr:8989") ||
		!strings.Contains(string(out), "api_key: key-http://sonarr:8989") {
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
