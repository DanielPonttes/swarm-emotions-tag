#!/usr/bin/env bash
set -euo pipefail

if [[ "${EUID}" -eq 0 ]]; then
  echo "Execute este script como usuario comum com sudo disponivel, nao como root."
  exit 1
fi

if ! command -v sudo >/dev/null 2>&1; then
  echo "sudo nao encontrado."
  exit 1
fi

. /etc/os-release
CODENAME="${UBUNTU_CODENAME:-${VERSION_CODENAME:-}}"
if [[ -z "${CODENAME}" ]]; then
  echo "Nao foi possivel detectar o codename do Ubuntu."
  exit 1
fi

echo "Removendo pacotes conflitantes, se existirem..."
sudo apt remove -y docker.io docker-compose docker-compose-v2 docker-doc podman-docker containerd runc || true

echo "Instalando dependencias basicas..."
sudo apt update
sudo apt install -y ca-certificates curl

echo "Configurando repositorio oficial do Docker para Ubuntu ${CODENAME}..."
sudo install -m 0755 -d /etc/apt/keyrings
sudo curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
sudo chmod a+r /etc/apt/keyrings/docker.asc
sudo tee /etc/apt/sources.list.d/docker.sources >/dev/null <<EOF
Types: deb
URIs: https://download.docker.com/linux/ubuntu
Suites: ${CODENAME}
Components: stable
Signed-By: /etc/apt/keyrings/docker.asc
EOF

echo "Instalando Docker Engine, Buildx e Compose plugin..."
sudo apt update
sudo apt install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin

echo "Habilitando servico do Docker..."
sudo systemctl enable --now docker

echo "Adicionando o usuario ${USER} ao grupo docker..."
sudo usermod -aG docker "${USER}"

echo "Verificando versoes instaladas..."
docker --version || true
docker compose version || true
sudo docker run --rm hello-world

cat <<'EOF'

Docker instalado.

Passos finais:
1. Encerre a sessao do shell atual e abra outra, ou rode: newgrp docker
2. Valide acesso sem sudo: docker ps
3. Suba o Qdrant deste repositorio: ./scripts/up_qdrant.sh

EOF
