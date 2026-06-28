package cmd

import (
	"fmt"
	"aeroflare/src/auth"
	"github.com/charmbracelet/huh"
)

const githubClientID = "Ov23liIJyLpd2Cse5gne"

func runInteractiveAuth() {
	var service string
	
	err := huh.NewSelect[string]().
		Title("What do you want to authenticate?").
		Options(
			huh.NewOption("GitHub / GitLab", "github"),
			huh.NewOption("Cloudflare", "cloudflare"),
			huh.NewOption("Custom OCI Registry", "oci"),
		).
		Value(&service).
		Run()
		
	if err != nil {
		PrintError("Cancelled")
		return
	}

	manager := getSecretsManager()

	switch service {
	case "github":
		var ghMethod string
		err = huh.NewSelect[string]().
			Title("How would you like to authenticate?").
			Options(
				huh.NewOption("Device Auth Flow (Browser)", "device"),
				huh.NewOption("Enter Token Manually", "manual"),
			).
			Value(&ghMethod).
			Run()
		if err != nil {
			return
		}

		var token string
		if ghMethod == "device" {
			fmt.Println("Requesting device code...")
			res, err := auth.RequestDeviceCode(githubClientID)
			if err != nil {
				PrintError(fmt.Sprintf("Failed to request code: %v", err))
				return
			}
			fmt.Printf("Please go to %s and enter the code: %s\n", res.VerificationURI, res.UserCode)
			fmt.Println("Waiting for authorization...")
			
			token, err = auth.PollAccessToken(githubClientID, res.DeviceCode, res.Interval)
			if err != nil {
				PrintError(fmt.Sprintf("Authorization failed: %v", err))
				return
			}
		} else {
			huh.NewInput().Title("GitHub / GitLab Token").EchoMode(huh.EchoModePassword).Value(&token).Run()
		}
		
		if token != "" {
			if err := manager.Set("github-token", token); err != nil {
				PrintError(fmt.Sprintf("Failed to save token: %v", err))
				return
			}
			fmt.Println("Success! Token saved. This will automatically be used for GitHub APIs and the ghcr.io container registry.")
		}

	case "cloudflare":
		var apiToken, userID string
		huh.NewForm(
			huh.NewGroup(
				huh.NewInput().Title("Cloudflare API Token").EchoMode(huh.EchoModePassword).Value(&apiToken),
				huh.NewInput().Title("Cloudflare User ID").Value(&userID),
			),
		).Run()

		if apiToken != "" {
			manager.Set("cf-token", apiToken)
		}
		if userID != "" {
			manager.Set("cf-user-id", userID)
		}
		fmt.Println("Cloudflare credentials saved.")

	case "oci":
		var registry, user, pass string
		huh.NewForm(
			huh.NewGroup(
				huh.NewInput().Title("Registry URL (e.g. registry.gitlab.com)").Value(&registry),
				huh.NewInput().Title("Username").Value(&user),
				huh.NewInput().Title("Token / Password").EchoMode(huh.EchoModePassword).Value(&pass),
			),
		).Run()

		if registry != "" {
			manager.Set(fmt.Sprintf("oci-%s-username", registry), user)
			manager.Set(fmt.Sprintf("oci-%s-token", registry), pass)
			fmt.Println("OCI credentials saved.")
		}
	}
}
