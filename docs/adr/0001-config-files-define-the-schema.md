# Config files define the schema

A Config's Items (their names, types, descriptions and Defaults) are defined in a config file in the repository of the code that reads it, and code is generated from that file. Mimir's server stores only Overrides; it never defines or edits a schema. This keeps the schema versioned and reviewed alongside the code that depends on it, and means a Deployable always has a usable value for every Item, even when Mimir holds nothing for it.
