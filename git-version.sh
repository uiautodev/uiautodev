#!/usr/bin/env bash

set -e

# Check if HEAD is already tagged
if git describe --exact-match --tags HEAD >/dev/null 2>&1; then
  tag=$(git describe --tags --exact-match)
  echo "✅ Current commit is already tagged: $tag"
  echo "$tag" > version.txt
  exit 0
fi

# Get latest tag (fallback to 0.0.0)
latest_tag=$(git tag --sort=-v:refname | head -n 1)
latest_tag=${latest_tag:-0.0.0}

echo "Latest tag: $latest_tag"

# Parse semver (x.y.z)
IFS='.' read -r major minor patch <<< "$latest_tag"

# Default: bump patch
next_tag="$major.$minor.$((patch + 1))"

# Ask user
read -p "Enter new tag [$next_tag]: " input_tag
new_tag=${input_tag:-$next_tag}

# Create tag
git tag "$new_tag"

# Write version file
echo "$new_tag" > version.txt

echo "🏷️  Created tag: $new_tag"
echo "📝 Wrote version.txt"
