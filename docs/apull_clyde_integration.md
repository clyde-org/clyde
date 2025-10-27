#### Introduction
Clyde is a peer-to-peer container image disemmination system. This document explains the mechanisms built into Apull for integrating with Clyde.

#### Configuration
Apull runs as a system daemon process on a specific node
Clyde runs as a pod instance spawned by Kubernetes on the same node, and may interact with other peers (e.g., instances running on remote nodes)
Containerd redirects container image layer/blob requests to Clyde locally via `127.0.0.1:30021``
Integration Workflow
The following steps describe the workflow of the integration between Apull and Clyde

#### Startup
Once the Apull system daemon starts, it reads the content of its configuration file placed under /etc/apull/config.toml and identifies a value that indicates if Apull should integrate with Clyde, and it also identifies the endpoint address of the instance of Clyde running on the same node

#### Communication with Clyde
Apull attempts to respond to pull requests by directing them to Clyde at first instance. This is achieved as follows:

Apull constructs an OCI-compatible HTTP request to Clyde for partial content for fragments of data of a specific blob/layer of a particular container image. The image layer can be identified by its digest (e.g., hash or layer id), whereas the byte range is specified in the HTTP request's header.
Clyde performs a lookup in its local registry for the specific image layer, and returns an HTTP response of 206 if it finds the content.
If Clyde does not find the image locally, then it will interact with its peers on remote hosts to obtain the data requested. If the image is not found within the peer-to-peer network, then Clyde will attempt to pull the image from a remote repository

If Clyde returns a 404 HTTP response, then Apull will fall back to make the same request for data from a remote repository directly (e.g., a harbor endpoint).
The implementation logic in Clyde processes the data returned from Clyde or a remote repository normally.
Please note that Clyde has been modified accordingly to support incoming HTTP requests for partial data (e.g., with status code 206)