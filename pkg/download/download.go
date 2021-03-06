package download

import (
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"

	"github.com/mholt/archiver"
	"github.com/pkg/errors"
	"github.com/replicatedhq/kots/pkg/auth"
	"github.com/replicatedhq/kots/pkg/k8sutil"
	"github.com/replicatedhq/kots/pkg/logger"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

type DownloadOptions struct {
	Namespace             string
	KubernetesConfigFlags *genericclioptions.ConfigFlags
	Overwrite             bool
	Silent                bool
	DecryptPasswordValues bool
}

func Download(appSlug string, path string, downloadOptions DownloadOptions) error {
	log := logger.NewLogger()
	if downloadOptions.Silent {
		log.Silence()
	}

	log.ActionWithSpinner("Connecting to cluster")

	clientset, err := k8sutil.GetClientset(downloadOptions.KubernetesConfigFlags)
	if err != nil {
		log.FinishSpinnerWithError()
		return errors.Wrap(err, "failed to get clientset")
	}

	podName, err := k8sutil.FindKotsadm(clientset, downloadOptions.Namespace)
	if err != nil {
		log.FinishSpinnerWithError()
		return errors.Wrap(err, "failed to find kotsadm pod")
	}

	stopCh := make(chan struct{})
	defer close(stopCh)

	localPort, errChan, err := k8sutil.PortForward(downloadOptions.KubernetesConfigFlags, 0, 3000, downloadOptions.Namespace, podName, false, stopCh, log)
	if err != nil {
		log.FinishSpinnerWithError()
		return errors.Wrap(err, "failed to start port forwarding")
	}

	go func() {
		select {
		case err := <-errChan:
			if err != nil {
				log.Error(err)
			}
		case <-stopCh:
		}
	}()

	authSlug, err := auth.GetOrCreateAuthSlug(downloadOptions.KubernetesConfigFlags, downloadOptions.Namespace)
	if err != nil {
		log.FinishSpinnerWithError()
		return errors.Wrap(err, "failed to get kotsadm auth slug")
	}

	url := fmt.Sprintf("http://localhost:%d/api/v1/download?slug=%s", localPort, appSlug)
	if downloadOptions.DecryptPasswordValues {
		url = fmt.Sprintf("%s&decryptPasswordValues=1", url)
	}

	newRequest, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.FinishSpinnerWithError()
		return errors.Wrap(err, "failed to create download request")
	}
	newRequest.Header.Add("Authorization", authSlug)

	resp, err := http.DefaultClient.Do(newRequest)
	if err != nil {
		log.FinishSpinnerWithError()
		return errors.Wrap(err, "failed to get from kotsadm")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.FinishSpinnerWithError()
		return errors.Errorf("unexpected status code from %s: %s", url, resp.Status)
	}

	tmpFile, err := ioutil.TempFile("", "kots")
	if err != nil {
		log.FinishSpinner()
		return errors.Wrap(err, "failed to create temp file")
	}
	defer os.Remove(tmpFile.Name())

	_, err = io.Copy(tmpFile, resp.Body)
	if err != nil {
		log.FinishSpinner()
		return errors.Wrap(err, "failed to write archive")
	}
	tmpFile.Close()

	// Delete the destination, if needed and requested
	if _, err := os.Stat(path); err == nil {
		if downloadOptions.Overwrite {
			if err := os.RemoveAll(path); err != nil {
				return errors.Wrap(err, "failed to delete existing download")
			}
		} else {
			log.FinishSpinner()
			log.ActionWithoutSpinner("")
			log.Error(errors.Errorf("Directory %s already exists. You can re-run this command with --overwrite to automatically overwrite it", path))
			log.ActionWithoutSpinner("")
			return errors.Errorf("directory already exists at %s", path)
		}
	}

	tarGz := archiver.TarGz{
		Tar: &archiver.Tar{
			ImplicitTopLevelFolder: false,
		},
	}
	if err := tarGz.Unarchive(tmpFile.Name(), path); err != nil {
		return errors.Wrap(err, "failed to extract tar gz")
	}

	log.FinishSpinner()

	return nil
}
