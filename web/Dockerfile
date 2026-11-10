# Веб DocFinder: Node.js без внешних зависимостей, только статика и прокси в ядро.
FROM node:24-alpine

ENV NODE_ENV=production \
	WEB_HOST=0.0.0.0 \
	WEB_PORT=3000

WORKDIR /app

# Зависимостей нет, копируем только то, что нужно для запуска
COPY package.json ./
COPY server ./server
COPY public ./public

USER node
EXPOSE 3000

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
	CMD node -e "fetch('http://127.0.0.1:' + (process.env.WEB_PORT || 3000) + '/healthz').then((r) => process.exit(r.ok ? 0 : 1)).catch(() => process.exit(1))"

CMD ["node", "server/index.js"]
