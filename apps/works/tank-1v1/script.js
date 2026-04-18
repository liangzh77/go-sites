(function() {
    const canvasA = document.getElementById('canvasA');
    const canvasB = document.getElementById('canvasB');
    const hpALabel = document.getElementById('hpA');
    const hpBLabel = document.getElementById('hpB');
    const scoreALabel = document.getElementById('scoreA');
    const scoreBLabel = document.getElementById('scoreB');

    // Resize canvases to device pixels for crisp rendering
    const resize = () => {
        const dpr = Math.max(1, Math.min(2, window.devicePixelRatio || 1));
        for (const c of [canvasA, canvasB]) {
            const rect = c.getBoundingClientRect();
            c.width = Math.floor(rect.width * dpr);
            c.height = Math.floor(rect.height * dpr);
            c._dpr = dpr;
        }
    };
    window.addEventListener('resize', resize);
    resize();

    const world = { width: 2000, height: 1200, obstacles: [] };

    // 固定重生点与安全半径，生成障碍时避让此区域
    const SPAWN_A = { x: 300, y: world.height - 300 };
    const SPAWN_B = { x: world.width - 300, y: 300 };
    const SPAWN_SAFE_RADIUS = 140;

    function rectsOverlap(a, b, margin) {
        const m = margin || 0;
        const ax1 = a.x - m, ay1 = a.y - m, ax2 = a.x + a.w + m, ay2 = a.y + a.h + m;
        const bx1 = b.x - m, by1 = b.y - m, bx2 = b.x + b.w + m, by2 = b.y + b.h + m;
        return !(ax2 <= bx1 || bx2 <= ax1 || ay2 <= by1 || by2 <= ay1);
    }

    function generateObstacles() {
        const obstacles = [];
        const count = 8;
        const margin = 12; // 墙体之间留缝
        let attempts = 0, i = 0;
        while (i < count && attempts < 2000) {
            attempts++;
            const w = 120 + Math.random() * 180;
            const h = 80 + Math.random() * 140;
            const x = 150 + Math.random() * (world.width - 300 - w);
            const y = 150 + Math.random() * (world.height - 300 - h);
            const cand = { x, y, w, h };

            // 避免与已有墙体重叠
            let overlap = false;
            for (const o of obstacles) {
                if (rectsOverlap(cand, o, margin)) { overlap = true; break; }
            }
            if (overlap) continue;

            // 避免覆盖重生安全区
            if (circleRectCollide(SPAWN_A.x, SPAWN_A.y, SPAWN_SAFE_RADIUS, cand.x, cand.y, cand.w, cand.h)) continue;
            if (circleRectCollide(SPAWN_B.x, SPAWN_B.y, SPAWN_SAFE_RADIUS, cand.x, cand.y, cand.w, cand.h)) continue;

            obstacles.push(cand);
            i++;
        }
        return obstacles;
    }

    world.obstacles = generateObstacles();

    const makePlayer = (opts) => ({
        id: opts.id,
        x: opts.x,
        y: opts.y,
        radius: 20,
        speed: 280,
        color: opts.color,
        maxHp: 1000,
        hp: 1000,
        alive: true,
        respawnAt: 0,
        score: 0,
        aimX: opts.x,
        aimY: opts.y,
        recoil: 0
    });

    const playerA = makePlayer({ id: 'A', x: SPAWN_A.x, y: SPAWN_A.y, color: '#74d3ff' });
    const playerB = makePlayer({ id: 'B', x: SPAWN_B.x, y: SPAWN_B.y, color: '#ff7aa8' });

    const bullets = [];
    const bulletSpeed = 900;

    const keys = new Set();
    window.addEventListener('keydown', (e) => {
        if (['Space', 'Enter', 'KeyW', 'KeyA', 'KeyS', 'KeyD', 'KeyI', 'KeyJ', 'KeyK', 'KeyL'].includes(e.code)) {
            e.preventDefault();
        }
        keys.add(e.code);
    });
    window.addEventListener('keyup', (e) => { keys.delete(e.code); });

    // B 玩家不再使用鼠标控制，改为键盘 I/J/K/L 与 Right Shift 射击

    function clamp(v, min, max) { return Math.max(min, Math.min(max, v)); }

    function circleRectCollide(cx, cy, r, rx, ry, rw, rh) {
        const nx = clamp(cx, rx, rx + rw);
        const ny = clamp(cy, ry, ry + rh);
        const dx = cx - nx;
        const dy = cy - ny;
        return dx * dx + dy * dy <= r * r;
    }

    function lineRectIntersect(x1, y1, x2, y2, rx, ry, rw, rh) {
        // Cohen–Sutherland style quick reject using Liang–Barsky line clipping
        let t0 = 0, t1 = 1;
        const dx = x2 - x1, dy = y2 - y1;
        const p = [-dx, dx, -dy, dy];
        const q = [x1 - rx, rx + rw - x1, y1 - ry, ry + rh - y1];
        for (let i = 0; i < 4; i++) {
            if (p[i] === 0) { if (q[i] < 0) return false; }
            else {
                const r = q[i] / p[i];
                if (p[i] < 0) { if (r > t1) return false; if (r > t0) t0 = r; }
                else { if (r < t0) return false; if (r < t1) t1 = r; }
            }
        }
        return true;
    }

    function movePlayer(p, dt) {
        if (!p.alive) return;
        let vx = 0, vy = 0;
        if (p.id === 'A') {
            if (keys.has('KeyA')) vx -= 1;
            if (keys.has('KeyD')) vx += 1;
            if (keys.has('KeyW')) vy -= 1;
            if (keys.has('KeyS')) vy += 1;
        } else if (p.id === 'B') {
            if (keys.has('KeyJ')) vx -= 1; // J 左
            if (keys.has('KeyL')) vx += 1; // L 右
            if (keys.has('KeyI')) vy -= 1; // I 上
            if (keys.has('KeyK')) vy += 1; // K 下
        }
        const len = Math.hypot(vx, vy) || 1;
        vx = vx / len * p.speed * dt;
        vy = vy / len * p.speed * dt;

        // tentative position
        let nx = p.x + vx;
        let ny = p.y + vy;

        // collide with world bounds
        nx = clamp(nx, p.radius, world.width - p.radius);
        ny = clamp(ny, p.radius, world.height - p.radius);

        // collide with obstacles (simple push out)
        for (const o of world.obstacles) {
            if (circleRectCollide(nx, ny, p.radius, o.x, o.y, o.w, o.h)) {
                // push out on shallow axis
                const left = nx - o.x;
                const right = (o.x + o.w) - nx;
                const top = ny - o.y;
                const bottom = (o.y + o.h) - ny;
                const minX = Math.min(left, right);
                const minY = Math.min(top, bottom);
                if (minX < minY) {
                    nx += left < right ? -(p.radius - left) : (p.radius - right);
                } else {
                    ny += top < bottom ? -(p.radius - top) : (p.radius - bottom);
                }
            }
        }

        p.x = nx; p.y = ny;
    }

    function tryFire(p, now) {
        if (!p.alive) return;
        const dx = p.aimX - p.x;
        const dy = p.aimY - p.y;
        let angle = Math.atan2(dy, dx);
        // small recoil jitter
        const jitter = (Math.random() - 0.5) * 0.02 * (1 + p.recoil);
        angle += jitter;
        const vx = Math.cos(angle) * bulletSpeed;
        const vy = Math.sin(angle) * bulletSpeed;
        // 一次开火，发射两发
        bullets.push({ x: p.x, y: p.y, vx, vy, owner: p.id, life: 1.6 });
        bullets.push({ x: p.x, y: p.y, vx, vy, owner: p.id, life: 1.6 });
        p.recoil = Math.min(1, p.recoil + 0.08);
    }

    window.addEventListener('keydown', (e) => {
        if (e.code === 'Space') {
            tryFire(playerA, Date.now());
        } else if (e.code === 'Enter') {
            tryFire(playerB, Date.now());
        }
    });

    function updateBullets(dt) {
        for (let i = bullets.length - 1; i >= 0; i--) {
            const b = bullets[i];
            b.x += b.vx * dt;
            b.y += b.vy * dt;
            b.life -= dt;
            // remove if out or life end
            if (b.x < -100 || b.x > world.width + 100 || b.y < -100 || b.y > world.height + 100 || b.life <= 0) {
                bullets.splice(i, 1); continue;
            }
            // collide with obstacles
            let hitObstacle = false;
            for (const o of world.obstacles) {
                if (b.x >= o.x && b.x <= o.x + o.w && b.y >= o.y && b.y <= o.y + o.h) { hitObstacle = true; break; }
            }
            if (hitObstacle) { bullets.splice(i, 1); continue; }

            // collide with players
            const target = b.owner === 'A' ? playerB : playerA;
            if (target.alive) {
                const dist2 = (b.x - target.x) ** 2 + (b.y - target.y) ** 2;
                if (dist2 <= (target.radius + 2) ** 2) {
                    damage(target, 20, b.owner);
                    bullets.splice(i, 1);
                }
            }
        }
    }

    function damage(target, amount, attackerId) {
        if (!target.alive) return;
        target.hp = Math.max(0, target.hp - amount);
        if (target.hp === 0) {
            target.alive = false;
            target.respawnAt = performance.now() + 2000;
            const attacker = attackerId === 'A' ? playerA : playerB;
            attacker.score += 1;
            updateHUD();
        } else {
            updateHUD();
        }
    }

    function respawnIfNeeded(p) {
        if (p.alive) return;
        if (performance.now() >= p.respawnAt) {
            p.hp = p.maxHp;
            p.alive = true;
            // 固定到安全重生点（已在障碍生成时避让）
            if (p.id === 'A') { p.x = SPAWN_A.x; p.y = SPAWN_A.y; }
            else { p.x = SPAWN_B.x; p.y = SPAWN_B.y; }
            p.recoil = 0;
            updateHUD();
        }
    }

    function updateHUD() {
        hpALabel.textContent = `HP: ${playerA.alive ? playerA.hp : 0}`;
        hpBLabel.textContent = `HP: ${playerB.alive ? playerB.hp : 0}`;
        scoreALabel.textContent = `Score: ${playerA.score}`;
        scoreBLabel.textContent = `Score: ${playerB.score}`;
    }

    function getCamera(p, canvas) {
        // camera centers on player, clamped to world bounds
        const viewW = canvas.width;
        const viewH = canvas.height;
        const scale = 1.0; // could add zoom effects later
        const halfW = viewW / scale / 2;
        const halfH = viewH / scale / 2;
        const x = clamp(p.x - halfW, 0, world.width - 2 * halfW);
        const y = clamp(p.y - halfH, 0, world.height - 2 * halfH);
        return { x, y, scale };
    }

    function drawScene(ctx, canvas, me, enemy) {
        const cam = getCamera(me, canvas);
        ctx.save();
        ctx.clearRect(0, 0, canvas.width, canvas.height);
        ctx.scale(cam.scale, cam.scale);
        ctx.translate(-cam.x, -cam.y);

        // Background grid
        drawGrid(ctx);

        // Obstacles
        for (const o of world.obstacles) {
            ctx.fillStyle = 'rgba(255,255,255,0.08)';
            ctx.strokeStyle = 'rgba(255,255,255,0.15)';
            ctx.lineWidth = 2;
            roundRect(ctx, o.x, o.y, o.w, o.h, 12);
            ctx.fill();
            ctx.stroke();
        }

        // Bullets
        for (const b of bullets) {
            // draw if inside view
            if (b.x >= cam.x - 10 && b.x <= cam.x + canvas.width / cam.scale + 10 &&
                b.y >= cam.y - 10 && b.y <= cam.y + canvas.height / cam.scale + 10) {
                ctx.fillStyle = b.owner === 'A' ? '#74d3ff' : '#ff7aa8';
                ctx.beginPath();
                ctx.arc(b.x, b.y, 3, 0, Math.PI * 2);
                ctx.fill();
            }
        }

        // Players
        drawPlayer(ctx, me, true);
        drawPlayer(ctx, enemy, false);

        // Crosshair (for me)
        const aimDx = me.aimX - me.x;
        const aimDy = me.aimY - me.y;
        const aimLen = Math.hypot(aimDx, aimDy) || 1;
        const hx = me.x + (aimDx / aimLen) * 140;
        const hy = me.y + (aimDy / aimLen) * 140;
        drawCrosshair(ctx, hx, hy, me.id === 'A' ? '#74d3ff' : '#ff7aa8');

        // Vignette overlay
        ctx.restore();
        drawVignette(ctx, canvas);
    }

    function drawPlayer(ctx, p, isSelf) {
        ctx.save();
        const base = isSelf ? 'rgba(116,211,255,0.25)' : 'rgba(255,122,168,0.25)';
        const edge = isSelf ? '#74d3ff' : '#ff7aa8';
        ctx.fillStyle = base;
        ctx.strokeStyle = edge;
        ctx.lineWidth = 3;
        ctx.beginPath();
        ctx.arc(p.x, p.y, p.radius, 0, Math.PI * 2);
        ctx.fill();
        ctx.stroke();

        // Direction line
        const dx = p.aimX - p.x, dy = p.aimY - p.y;
        const a = Math.atan2(dy, dx);
        ctx.strokeStyle = edge;
        ctx.lineWidth = 2;
        ctx.beginPath();
        ctx.moveTo(p.x, p.y);
        ctx.lineTo(p.x + Math.cos(a) * (p.radius + 14), p.y + Math.sin(a) * (p.radius + 14));
        ctx.stroke();

        // HP bar
        const bw = 60, bh = 8;
        const bx = p.x - bw / 2, by = p.y - p.radius - 16;
        ctx.fillStyle = 'rgba(255,255,255,0.15)';
        roundRect(ctx, bx, by, bw, bh, 4); ctx.fill();
        const pct = clamp(p.hp / p.maxHp, 0, 1);
        ctx.fillStyle = '#78e08f';
        roundRect(ctx, bx, by, bw * pct, bh, 4); ctx.fill();

        ctx.restore();
    }

    function drawCrosshair(ctx, x, y, color) {
        ctx.save();
        ctx.strokeStyle = color;
        ctx.lineWidth = 2;
        ctx.beginPath();
        const s = 12;
        ctx.moveTo(x - s, y); ctx.lineTo(x - 3, y);
        ctx.moveTo(x + 3, y); ctx.lineTo(x + s, y);
        ctx.moveTo(x, y - s); ctx.lineTo(x, y - 3);
        ctx.moveTo(x, y + 3); ctx.lineTo(x, y + s);
        ctx.stroke();
        ctx.beginPath();
        ctx.arc(x, y, 2.5, 0, Math.PI * 2);
        ctx.stroke();
        ctx.restore();
    }

    function drawGrid(ctx) {
        ctx.save();
        ctx.fillStyle = '#0e0f1f';
        ctx.fillRect(0, 0, world.width, world.height);
        const step = 80;
        ctx.strokeStyle = 'rgba(255,255,255,0.06)';
        ctx.lineWidth = 1;
        ctx.beginPath();
        for (let x = 0; x <= world.width; x += step) {
            ctx.moveTo(x, 0); ctx.lineTo(x, world.height);
        }
        for (let y = 0; y <= world.height; y += step) {
            ctx.moveTo(0, y); ctx.lineTo(world.width, y);
        }
        ctx.stroke();
        ctx.restore();
    }

    function roundRect(ctx, x, y, w, h, r) {
        ctx.beginPath();
        ctx.moveTo(x + r, y);
        ctx.arcTo(x + w, y, x + w, y + h, r);
        ctx.arcTo(x + w, y + h, x, y + h, r);
        ctx.arcTo(x, y + h, x, y, r);
        ctx.arcTo(x, y, x + w, y, r);
        ctx.closePath();
    }

    function drawVignette(ctx, canvas) {
        const g = ctx.createRadialGradient(
            canvas.width * 0.5, canvas.height * 0.5, Math.min(canvas.width, canvas.height) * 0.35,
            canvas.width * 0.5, canvas.height * 0.5, Math.max(canvas.width, canvas.height) * 0.65
        );
        g.addColorStop(0, 'rgba(0,0,0,0)');
        g.addColorStop(1, 'rgba(0,0,0,0.45)');
        ctx.fillStyle = g;
        ctx.fillRect(0, 0, canvas.width, canvas.height);
    }

    function updateAimFromKeyboard(p, dt) {
        // 准星朝向当前移动方向；未移动时保留原方向，并进行平滑插值
        let ax = 0, ay = 0;
        if (p.id === 'A') {
            if (keys.has('KeyA')) ax -= 1;
            if (keys.has('KeyD')) ax += 1;
            if (keys.has('KeyW')) ay -= 1;
            if (keys.has('KeyS')) ay += 1;
        } else if (p.id === 'B') {
            if (keys.has('KeyJ')) ax -= 1;
            if (keys.has('KeyL')) ax += 1;
            if (keys.has('KeyI')) ay -= 1;
            if (keys.has('KeyK')) ay += 1;
        }
        if (ax !== 0 || ay !== 0) {
            const len = Math.hypot(ax, ay) || 1;
            const targetX = p.x + (ax / len) * 400;
            const targetY = p.y + (ay / len) * 400;
            const smoothing = 12; // 越大越快
            const alpha = 1 - Math.exp(-smoothing * dt);
            p.aimX = p.aimX + (targetX - p.aimX) * alpha;
            p.aimY = p.aimY + (targetY - p.aimY) * alpha;
        }
    }

    function checkWin() {
        const winScore = 10;
        if (playerA.score >= winScore || playerB.score >= winScore) {
            const winner = playerA.score > playerB.score ? 'A' : 'B';
            alert(`玩家 ${winner} 获胜！`);
            playerA.score = 0; playerB.score = 0;
            playerA.hp = playerA.maxHp; playerB.hp = playerB.maxHp;
            playerA.alive = true; playerB.alive = true;
            updateHUD();
        }
    }

    let last = performance.now();
    function frame(now) {
        const dt = Math.min(0.033, (now - last) / 1000);
        last = now;
        playerA.recoil = Math.max(0, playerA.recoil - dt * 1.2);
        playerB.recoil = Math.max(0, playerB.recoil - dt * 1.2);

        movePlayer(playerA, dt);
        movePlayer(playerB, dt);
        updateAimFromKeyboard(playerA, dt);
        updateAimFromKeyboard(playerB, dt);
        updateBullets(dt);
        respawnIfNeeded(playerA);
        respawnIfNeeded(playerB);
        checkWin();

        const ctxA = canvasA.getContext('2d');
        const ctxB = canvasB.getContext('2d');
        drawScene(ctxA, canvasA, playerA, playerB);
        drawScene(ctxB, canvasB, playerB, playerA);

        requestAnimationFrame(frame);
    }
    requestAnimationFrame(frame);
    updateHUD();
})();


