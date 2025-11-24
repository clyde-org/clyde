package state

// Structure used for tracking missing keys with their image context
type KeySource struct {

	// Note that this information to construct an http request to an arbitrary remote peer for retrieving container image content
	PeerAddr string
	ImageName string
	Registry string
}

// Structure for holding data obtained from remote peers in json format to handle the processing of such data carefully
type ImageLayers struct {

	// Note that that this information is used to obtain clues (e.g., metadata about locally stored content) which may be provided to remote peers
	ImageName 	string 		`json:"image_name"`
	Registry 	string		`json:"registry"`
	LayerKeys	[]string	`json:"layer_keys"`
	Tag			string		`json:"tag"`
	Digest		string		`json:"digest"`
}
