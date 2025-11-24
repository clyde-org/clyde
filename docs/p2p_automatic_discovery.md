# Peer-to-peer Discovery, and Automatic Content Distribution

This is an experimental feature that aims to improve content distribution and retrieval across peers. This is acheived by implementing the ability for peers to discover each other and automatically share specific container image content.

## Discovery

Peers advertise their presence to a global peer index. For example, an arbitrary peer advertises its own endpoint address to a pre-known key that can be resolved by other peers, allowing them to obtain the endpoint address. New peers joining the network will automatically advertise for their presence using the peer index and will be discoverable also by other peers. This discovery mechanism is implemented within a periodic synchronisation operation that is performed by each peer for a pre-configured duration.

## Router Interface 

The peer-to-peer router interface has been extended to provide additional APIs, these include ServeKeys and FetchPeerKeys which support direct requests from peers.

- Serving Requests: Metadata can be requested from peers to obtain clues about the container image contents they maintain. For example, an arbitrary peer can invoke ServeKeys from a remote peer to retrieve such metadata (e.g., container images and associated layers). The response from the remote peer will be returned in a JSON-formatted message. Typically, serve requests are performed during periodic synchronisation mechanisms after peers have discovered their presence in order to obtain clues about the data they provide.

- Fetch Requests: An arbitrary peer can invoke FetchPeerKeys from a remote peer to obtain specific content (e.g., a container image layer or blob). Once the data has been recieved, it is then stored directly on the node at the default location where container images are maintained for example. Typically, fetch requests are performed by a peer that intends to retrieve a specific content form a remote peer after it has identified that such content is missing from its local storage.

## Parallel Downloads

- Some go routines have been implemented to enable a peer to obtain multiple layers or blobs related to a specific container image from different peers, which may be identified as peers that provide such content. Some components have been introduced to enable this behaviour in the system.

## Workflow

The following steps describe the behaviour of this experimental feature:

1. Peer presence advertisement: Peer 1 advertises its own address to a global peer index.
2. Peer discovery: Peer 2 will resolve the global peer index obtaining the endpoint address of peer 1 and any other peers in the network.
3. Metadata is requested from peers about the content they can provide. For example, peer 1 invokes peer 2 to get such metadata.
4. Peer 2 responds to peer 1 by returning a json-formatted string containing the metadata, and in turn peer 1 analyses the data received.
5. Peer 1 determines what is missing from its local storage (e.g., images, blobs) and identifies a number of potential peers that provide these layers, selects these peers (e.g., peer 2) and invokes them. 
6. Peer 2 returns the image blob that was requested by peer 1.
7. Upon obtaining the image blob from peer 2, peer 1 stores this content in its default local storage.
