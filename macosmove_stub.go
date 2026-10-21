//go:build !darwin

package main

func offerMoveToApplications() bool { return false }
