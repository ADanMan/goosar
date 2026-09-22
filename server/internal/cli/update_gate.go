package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/adanman/goosar/server/internal/deliveryprofile"
)

const deliveryProfileProbeTimeout = 5 * time.Second

func FetchServerDeliveryProfile(serverBaseURL string) (string, error) {
	client := &http.Client{Timeout: deliveryProfileProbeTimeout}
	url := strings.TrimRight(strings.TrimSpace(serverBaseURL), "/") + "/api/config"
	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}
	var конфиг struct {
		DeliveryProfile string `json:"delivery_profile"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&конфиг); err != nil {
		return "", err
	}
	return конфиг.DeliveryProfile, nil
}

func CheckSelfUpdateAllowed(serverBaseURL string) error {
	if deliveryprofile.IsPerimeterAdvertised(os.Getenv(deliveryprofile.EnvVar)) {
		return fmt.Errorf("self-update is disabled on this machine (%s=%s); updates are delivered by your operator", deliveryprofile.EnvVar, deliveryprofile.Perimeter)
	}
	if strings.TrimSpace(serverBaseURL) == "" {
		return nil
	}
	profile, err := FetchServerDeliveryProfile(serverBaseURL)
	if err != nil {

		return nil
	}
	if deliveryprofile.IsPerimeterAdvertised(profile) {
		return fmt.Errorf("self-update is disabled: the configured server (%s) runs the perimeter delivery profile; updates are delivered by your operator", serverBaseURL)
	}
	return nil
}
