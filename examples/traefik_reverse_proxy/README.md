# Traefik Reverse Proxy Example

This example illustrates a simple website served through Traefik.

## Launch

```sh
$ dockform plan
# Preview the resources that will be created

$ dockform apply
# Create the resources
```

In this setup, Traefik is configured to serve the website at `http://nginx.localhost`.

## Destroy

```sh
$ dockform destroy
# Remove all resources associated with this project
```