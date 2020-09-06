package dockerpromote

const Description = "Promotes a Docker image from one repository to another. Supported by local repositories only."

var Usage = []string{"jfrog rt docker-promote <source docker image> <source repo> <target repo>"}

const Arguments string = `	source docker image
		The docker image name to promote.
	source repo
		Source repository in Artifactory.
	target repo
		Target repository in Artifactory.
`
