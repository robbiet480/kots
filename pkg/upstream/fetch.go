package upstream

import (
	"net/url"

	"github.com/pkg/errors"
	"github.com/replicatedhq/kots/pkg/util"
)

type FetchOptions struct {
	HelmRepoName string
	HelmRepoURI  string
	LocalPath    string
	LicenseFile  string
}

func FetchUpstream(upstreamURI string, fetchOptions *FetchOptions) (*Upstream, error) {
	upstream, err := downloadUpstream(upstreamURI, fetchOptions)
	if err != nil {
		return nil, errors.Wrap(err, "download upstream failed")
	}

	return upstream, nil
}

func downloadUpstream(upstreamURI string, fetchOptions *FetchOptions) (*Upstream, error) {
	if !util.IsURL(upstreamURI) {
		return readFilesFromPath(upstreamURI)
	}

	u, err := url.ParseRequestURI(upstreamURI)
	if err != nil {
		return nil, errors.Wrap(err, "parse request uri failed")
	}
	if u.Scheme == "helm" {
		return downloadHelm(u, fetchOptions.HelmRepoURI)
	}
	if u.Scheme == "replicated" {
		return downloadReplicated(u, fetchOptions.LocalPath, fetchOptions.LicenseFile)
	}
	if u.Scheme == "file" {
		return readFilesFromURI(upstreamURI)
	}
	if u.Scheme == "git" {
		return downloadGit(upstreamURI)
	}
	if u.Scheme == "http" || u.Scheme == "https" {
		return downloadHttp(upstreamURI)
	}

	return nil, errors.Errorf("unknown protocol scheme %q", u.Scheme)
}
