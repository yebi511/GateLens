FROM node:24-alpine AS build
WORKDIR /src
COPY frontend/package*.json ./
RUN npm ci
COPY frontend/ ./
COPY assets/ /assets/
RUN npm run build

FROM nginxinc/nginx-unprivileged:1.27-alpine
ENV GATELENS_API_UPSTREAM=http://gatelens-api:8080 \
    NGINX_ENVSUBST_FILTER=GATELENS_API_UPSTREAM
COPY frontend/nginx/default.conf.template /etc/nginx/templates/default.conf.template
COPY --from=build /src/dist /usr/share/nginx/html
EXPOSE 8080
