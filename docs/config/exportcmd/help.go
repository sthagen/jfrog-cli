package exportcmd

const Description string = `Creates a server configuration token. The generated token can be imported by the "jfrog config import <Server token>" command.`

var Usage = []string{"jfrog config export <server token>"}

const Arguments string = `	server ID
		The configured server ID.`
