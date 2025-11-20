## Clyde

<div style="overflow: hidden;">
  <img src="docs/logo.jpg" 
       alt="Clyde Logo" 
       width="200" 
       style="float: left; margin-right: 15px; margin-bottom: 10px;">
  Clyde is a high-performance peer-to-peer (P2P) data acceleration engine built for rapid, large-scale delivery across diverse and distributed compute environments. Originally designed to optimize container image distribution across cluster nodes, Clyde now extends its capabilities to general content delivery including Huggingface models and Python (pip) packages. By leveraging intelligent peer discovery and local data sharing, Clyde dramatically reduces network overhead, speeds up deployment times, and enhances scalability for AI and cloud-native workloads.
</div>

## Architecture

![Clyde Architecture](docs/img/clyde-design.png "Clyde Architecture")

See more in the [design and architecture](./docs/design.md) guide.

## Main Features

1. **Container Image Distribution:** Clyde accelerates container image delivery across nodes through peer-to-peer sharing, reducing pull times and registry load.
2. **Hugging Face Model Distribution:** Large AI models are efficiently distributed using Clyde’s decentralized network, minimizing bandwidth and improving availability.
3. **Pip Package Distribution:** Python packages are fetched and shared locally within the cluster, enabling faster installs and reduced dependency on external repositories.
4. **Design Simplicity:** Clyde uses a simplified stateless design making it performant and easy to extend.
5. **Speed:** Data is cached locally on nodes and transmitted through the P2P network to enable faster delivery across the cluster.
6. **Saving:** Save bandwidth by serving content locally instead.
7. **Versatile:** Avoids rate-limiting and works even when external sources are down.

## Quick Motivating Results

For more details of the results see [Experiment Results](./docs/install.md#experiments)

| Container Images                                   | HuggingFace Models                          | pip Packages                                 |
| -------------------------------------------------- | ------------------------------------------- | -------------------------------------------- |
| <img src="docs/img/container_image.png" width="350"> | <img src="docs/img/huggingface.png" width="350"> | <img src="docs/img/pip.png" width="350"> |

## Design & Architecture

Follow the [design and architecture](./docs/design.md) to understand the design and architecture of Clyde.

## Build and Install

Please follow the building and installation instructions at [build](./docs/build.md) and install [guide](./docs/install.md) respectively to get started.

## Contribution

Read [contribution guidelines](./docs/contributing.md) for instructions on how to build and test Spegel.

## Acknowledgement

Many thanks to the developers of [Spegel](https://github.com/spegel-org/spegel) especially Philip.
