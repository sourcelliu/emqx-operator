#!/usr/bin/env bash
set -euo pipefail

# ensure dir
project_dir="$(dirname "$(readlink -f "$0")")"/..
cd -P -- "$project_dir"

# ensure kustomize
test -s "$project_dir/bin/kustomize" || make kustomize -C "$project_dir"

dir=$(mktemp -d)
# generate CRDs
pushd "$dir"
"$project_dir/bin/kustomize" build "$project_dir/config/crd" > crds.yaml
yq -s '"crd." + .metadata.name + ".yaml"' crds.yaml
popd

while IFS= read -r -d '' file
do
	sed -i '1i {{- if eq (include \"emqx-operator.renderCRDs\" .) \"true\" }}\n' "$file"
	echo -e '\n{{- end }}' >> "$file"
done <   <(find "$dir" -depth -type f -name "*.yaml" ! -name "crds.yaml" -print0)

find "$dir" -depth -type f -name "*.yaml" ! -name "crds.yaml" -exec mv {} "$project_dir/deploy/charts/emqx-operator/templates" \;
