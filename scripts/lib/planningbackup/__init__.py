"""Deployment backup schema-v2 primitives.

The package deliberately keeps the legacy v1 decoder behind a version dispatch
boundary.  It does not own application tables or planning-media lifecycle
rules; it only validates deployment artifacts and orchestrates their safety
gates.
"""

from .package import (
    V1_FILES,
    V2_FILES,
    ContractError,
    detect_schema,
    validate_package,
)

__all__ = ["V1_FILES", "V2_FILES", "ContractError", "detect_schema", "validate_package"]
