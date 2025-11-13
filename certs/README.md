# `certs/`

If your Keppel build needs to accept additional (e.g. company-internal) CA certificates,
put them into this folder as a `.crt` file, and `docker build` will pick them up automatically.
