// ShortGrass class (makes tanks/bullets semi-transparent)
class ShortGrass {
    constructor(x, y) {
        this.x = x;
        this.y = y;
    }
    draw() {
        ctx.fillStyle = '#7ec850';
        ctx.globalAlpha = 0.5;
        ctx.fillRect(this.x, this.y, WALL_SIZE, WALL_SIZE);
        ctx.globalAlpha = 1.0;
        ctx.strokeStyle = '#b6e388';
        ctx.strokeRect(this.x, this.y, WALL_SIZE, WALL_SIZE);
    }
    contains(x, y, size) {
        return x + size > this.x && x < this.x + WALL_SIZE && y + size > this.y && y < this.y + WALL_SIZE;
    }
}
// 2-Player Tank Battle Game
const canvas = document.getElementById('gameCanvas');
const ctx = canvas.getContext('2d');

const WIDTH = canvas.width;
const HEIGHT = canvas.height;

// Tank properties
const TANK_SIZE = 40;
const TANK_SPEED = 3;
const BULLET_SPEED = 7;
const BULLET_SIZE = 8;
const WALL_SIZE = 50;

// Directions
const DIRS = {
    UP: 0, RIGHT: 1, DOWN: 2, LEFT: 3
};

// Key mapping

const keys = {
    w: false, a: false, s: false, d: false, v: false, e: false, q: false, // Player 1
    p: false, l: false, ';': false, "'": false, m: false, Enter: false, Shift: false // Player 2 (custom keys)
};

document.addEventListener('keydown', e => { if (e.key in keys) keys[e.key] = true; });
document.addEventListener('keyup', e => { if (e.key in keys) keys[e.key] = false; });

// Tank class
class Tank {
    constructor(x, y, color, controls) {
        this.x = x;
        this.y = y;
        this.dir = DIRS.UP;
        this.color = color;
        this.controls = controls;
        this.bullets = [];
        this.cooldown = 0;
        this.dashCooldown = 0;
        this.specialBullets = [];
        this.specialCooldown = 0;
        // Teleport related
        this.teleportCooldown = 0; // frames
        this.teleportStandTime = 0; // frames standing on teleporter
    }
    specialAttack() {
        if (this.specialCooldown > 0) return;
        let bx = this.x + TANK_SIZE/2 - BULLET_SIZE/2;
        let by = this.y + TANK_SIZE/2 - BULLET_SIZE/2;
        let dx = 0, dy = 0;
        if (this.dir === DIRS.UP) dy = -BULLET_SPEED;
        else if (this.dir === DIRS.DOWN) dy = BULLET_SPEED;
        else if (this.dir === DIRS.LEFT) dx = -BULLET_SPEED;
        else if (this.dir === DIRS.RIGHT) dx = BULLET_SPEED;
        this.specialBullets.push({x: bx, y: by, dx, dy});
    this.specialCooldown = 600; // 10 seconds cooldown for special attack
    }
    dash() {
        if (this.dashCooldown > 0) return;
        // Move 2 tiles (2*WALL_SIZE) in current direction, can pass through walls
        let dx = 0, dy = 0;
        if (this.dir === DIRS.UP) dy = -2 * WALL_SIZE;
        else if (this.dir === DIRS.DOWN) dy = 2 * WALL_SIZE;
        else if (this.dir === DIRS.LEFT) dx = -2 * WALL_SIZE;
        else if (this.dir === DIRS.RIGHT) dx = 2 * WALL_SIZE;
        let nextX = Math.max(0, Math.min(WIDTH - TANK_SIZE, this.x + dx));
        let nextY = Math.max(0, Math.min(HEIGHT - TANK_SIZE, this.y + dy));
        this.x = nextX;
        this.y = nextY;
        this.dashCooldown = 60 * 60; // 60 seconds at 60fps
    }
    move() {
        let nextX = this.x, nextY = this.y, nextDir = this.dir;
        if (keys[this.controls.up]) { nextY -= TANK_SPEED; nextDir = DIRS.UP; }
        else if (keys[this.controls.down]) { nextY += TANK_SPEED; nextDir = DIRS.DOWN; }
        if (keys[this.controls.left]) { nextX -= TANK_SPEED; nextDir = DIRS.LEFT; }
        else if (keys[this.controls.right]) { nextX += TANK_SPEED; nextDir = DIRS.RIGHT; }
        // Boundaries
        nextX = Math.max(0, Math.min(WIDTH - TANK_SIZE, nextX));
        nextY = Math.max(0, Math.min(HEIGHT - TANK_SIZE, nextY));
        // Wall collision (solid + breakable)
        let collide = false;
        for (let w of [...walls, ...breakableWalls]) {
            if (nextX + TANK_SIZE > w.x && nextX < w.x + WALL_SIZE && nextY + TANK_SIZE > w.y && nextY < w.y + WALL_SIZE) {
                collide = true;
                break;
            }
        }
        if (!collide) {
            this.x = nextX;
            this.y = nextY;
        }
        this.dir = nextDir;
    }
    shoot() {
        if (this.cooldown === 0) {
            let bx = this.x + TANK_SIZE/2 - BULLET_SIZE/2;
            let by = this.y + TANK_SIZE/2 - BULLET_SIZE/2;
            let dx = 0, dy = 0;
            if (this.dir === DIRS.UP) dy = -BULLET_SPEED;
            else if (this.dir === DIRS.DOWN) dy = BULLET_SPEED;
            else if (this.dir === DIRS.LEFT) dx = -BULLET_SPEED;
            else if (this.dir === DIRS.RIGHT) dx = BULLET_SPEED;
            this.bullets.push({x: bx, y: by, dx, dy});
            this.cooldown = 20;
        }
    }
    updateBullets(walls) {
        for (let b of this.bullets) {
            b.x += b.dx;
            b.y += b.dy;
        }
        // Remove out-of-bounds or hit wall
        this.bullets = this.bullets.filter(b => {
            if (b.x < 0 || b.x > WIDTH || b.y < 0 || b.y > HEIGHT) return false;
            for (let w of walls) {
                if (b.x + BULLET_SIZE > w.x && b.x < w.x + WALL_SIZE && b.y + BULLET_SIZE > w.y && b.y < w.y + WALL_SIZE) return false;
            }
            return true;
        });
        // Special bullets move and only remove if out of bounds (ignore walls)
        for (let b of this.specialBullets) {
            b.x += b.dx;
            b.y += b.dy;
        }
        this.specialBullets = this.specialBullets.filter(b => b.x >= 0 && b.x <= WIDTH && b.y >= 0 && b.y <= HEIGHT);
        if (this.cooldown > 0) this.cooldown--;
        if (this.dashCooldown > 0) this.dashCooldown--;
        if (this.specialCooldown > 0) this.specialCooldown--;
        if (this.teleportCooldown > 0) this.teleportCooldown--;
    }
    draw() {
        ctx.save();
        ctx.translate(this.x + TANK_SIZE/2, this.y + TANK_SIZE/2);
        ctx.rotate(this.dir * Math.PI/2);
        ctx.fillStyle = this.color;
        ctx.fillRect(-TANK_SIZE/2, -TANK_SIZE/2, TANK_SIZE, TANK_SIZE);
        ctx.fillStyle = '#000';
        ctx.fillRect(-6, -TANK_SIZE/2, 12, 18); // Cannon
        ctx.restore();
        // Draw bullets
        ctx.fillStyle = '#ff0';
        for (let b of this.bullets) ctx.fillRect(b.x, b.y, BULLET_SIZE, BULLET_SIZE);
    }
}

// Wall class
class Wall {
    constructor(x, y) {
        this.x = x;
        this.y = y;
    }
    draw() {
        ctx.fillStyle = '#888';
        ctx.fillRect(this.x, this.y, WALL_SIZE, WALL_SIZE);
    }
}

// BreakableWall class (can be destroyed by bullets)
class BreakableWall {
    constructor(x, y) {
        this.x = x;
        this.y = y;
        this.hp = 1;
    }
    draw() {
        ctx.fillStyle = '#aa6';
        ctx.fillRect(this.x, this.y, WALL_SIZE, WALL_SIZE);
        ctx.strokeStyle = '#663';
        ctx.strokeRect(this.x+2, this.y+2, WALL_SIZE-4, WALL_SIZE-4);
    }
}

// Grass class (hides tanks/bullets inside)
class Grass {
    constructor(x, y) {
        this.x = x;
        this.y = y;
    }
    draw() {
        ctx.fillStyle = '#3c5';
        ctx.globalAlpha = 0.7;
        ctx.fillRect(this.x, this.y, WALL_SIZE, WALL_SIZE);
        ctx.globalAlpha = 1.0;
    }
    contains(x, y, size) {
        return x + size > this.x && x < this.x + WALL_SIZE && y + size > this.y && y < this.y + WALL_SIZE;
    }
}

// Teleporter class (stand 3s to teleport to its diagonal partner)
class Teleporter {
    constructor(x, y) {
        this.x = x;
        this.y = y;
        this.size = WALL_SIZE;
    }
    draw() {
        const cx = this.x + this.size/2;
        const cy = this.y + this.size/2;
        // Base pad
        ctx.save();
        const grd = ctx.createRadialGradient(cx, cy, 6, cx, cy, this.size/2);
        grd.addColorStop(0, '#7af');
        grd.addColorStop(1, '#135');
        ctx.fillStyle = grd;
        ctx.beginPath();
        ctx.arc(cx, cy, this.size/2 - 6, 0, Math.PI*2);
        ctx.fill();
        // Ring
        ctx.strokeStyle = '#8ef';
        ctx.lineWidth = 3;
        ctx.beginPath();
        ctx.arc(cx, cy, this.size/2 - 3, 0, Math.PI*2);
        ctx.stroke();
        ctx.restore();
    }
    contains(x, y, size) {
        return x + size > this.x && x < this.x + this.size && y + size > this.y && y < this.y + this.size;
    }
}

// Teleporter reserved positions (used for spawn rules)
const TELEPORTER_POSITIONS = [
    { x: WALL_SIZE * 2, y: WALL_SIZE * 2, size: WALL_SIZE },
    { x: WIDTH - WALL_SIZE * 3, y: HEIGHT - WALL_SIZE * 3, size: WALL_SIZE }
];

function isNearTeleporterRect(x, y, size, marginTiles) {
    const m = marginTiles * WALL_SIZE;
    for (let p of TELEPORTER_POSITIONS) {
        const rx1 = p.x - m, ry1 = p.y - m;
        const rx2 = p.x + p.size + m, ry2 = p.y + p.size + m;
        if (x + size > rx1 && x < rx2 && y + size > ry1 && y < ry2) return true;
    }
    return false;
}

// Game setup
const player1 = new Tank(100, HEIGHT/2, '#0f0', {up:'w',down:'s',left:'a',right:'d',shoot:'v'});
const player2 = new Tank(WIDTH-140, HEIGHT/2, '#f00', {up:'p',down:';',left:'l',right:"'",shoot:'m',dash:'Enter'});

// Walls (enhanced random layout)
const walls = [];
const grasses = [];
const shortGrasses = [];
const breakableWalls = [];
const teleporters = [];
// Border walls
for (let i = 0; i < WIDTH; i += WALL_SIZE) {
    walls.push(new Wall(i, 0));
    walls.push(new Wall(i, HEIGHT - WALL_SIZE));
}
for (let i = WALL_SIZE; i < HEIGHT - WALL_SIZE; i += WALL_SIZE) {
    walls.push(new Wall(0, i));
    walls.push(new Wall(WIDTH - WALL_SIZE, i));
}
// Random inner walls with wider corridors
function randomInt(a, b) { return Math.floor(Math.random() * (b - a + 1)) + a; }
let gridCols = Math.floor((WIDTH - WALL_SIZE*4) / WALL_SIZE);
let gridRows = Math.floor((HEIGHT - WALL_SIZE*4) / WALL_SIZE);
let wallCount = Math.floor(gridCols * gridRows * 0.13); // keep density similar
let minGap = TANK_SIZE * 2;
let grid = [];
for (let gx = 2; gx <= gridCols; gx++) {
    for (let gy = 2; gy <= gridRows; gy++) {
        grid.push({x: gx * WALL_SIZE, y: gy * WALL_SIZE});
    }
}
// Shuffle grid
for (let i = grid.length - 1; i > 0; i--) {
    let j = Math.floor(Math.random() * (i + 1));
    [grid[i], grid[j]] = [grid[j], grid[i]];
}
let placed = 0;
for (let cell of grid) {
    if (placed >= wallCount) break;
    let x = cell.x, y = cell.y;
    // Avoid spawn area
    if ((x < 200 && y > HEIGHT/2-100 && y < HEIGHT/2+100) || (x > WIDTH-250 && y > HEIGHT/2-100 && y < HEIGHT/2+100)) continue;
    // Avoid teleporter reserved margin (2 tiles around)
    if (isNearTeleporterRect(x, y, WALL_SIZE, 2)) continue;
    // Avoid too close to other walls
    let tooClose = false;
    for (let w of walls) {
        if (Math.abs(w.x - x) < minGap && Math.abs(w.y - y) < minGap) {
            tooClose = true;
            break;
        }
    }
    if (tooClose) continue;
    walls.push(new Wall(x, y));
    placed++;
}
// Center wall
walls.push(new Wall(WIDTH/2-WALL_SIZE/2, HEIGHT/2-WALL_SIZE/2));

// Add random breakable walls and grass tiles (not overlapping solid walls)
let candidates = grid.slice();
// breakable wall count as a small fraction of free cells
let breakableCount = Math.floor(grid.length * 0.06);
let grassCount = Math.floor(grid.length * 0.06);
let shortGrassCount = Math.floor(grid.length * 0.06);
for (let cell of candidates) {
    const x = cell.x, y = cell.y;
    if (walls.some(w => w.x === x && w.y === y)) continue;
    // Avoid blocking immediate spawn rectangles
    if ((x < 200 && y > HEIGHT/2-100 && y < HEIGHT/2+100) || (x > WIDTH-250 && y > HEIGHT/2-100 && y < HEIGHT/2+100)) continue;
    // Avoid teleporter reserved margin (2 tiles around)
    if (isNearTeleporterRect(x, y, WALL_SIZE, 2)) continue;
    if (breakableCount > 0) {
        breakableWalls.push(new BreakableWall(x, y));
        breakableCount--;
        continue;
    }
    if (grassCount > 0) {
        grasses.push(new Grass(x, y));
        grassCount--;
        continue;
    }
    if (shortGrassCount > 0) {
        shortGrasses.push(new ShortGrass(x, y));
        shortGrassCount--;
    }
    if (breakableCount <= 0 && grassCount <= 0 && shortGrassCount <= 0) break;
}

// Place two diagonal teleporters
const tp1 = new Teleporter(TELEPORTER_POSITIONS[0].x, TELEPORTER_POSITIONS[0].y);
const tp2 = new Teleporter(TELEPORTER_POSITIONS[1].x, TELEPORTER_POSITIONS[1].y);
teleporters.push(tp1, tp2);

function checkHit(tank, bullets, specialBullets) {
    for (let b of bullets) {
        if (b.x + BULLET_SIZE > tank.x && b.x < tank.x + TANK_SIZE && b.y + BULLET_SIZE > tank.y && b.y < tank.y + TANK_SIZE) {
            return true;
        }
    }
    for (let b of specialBullets) {
        if (b.x + BULLET_SIZE > tank.x && b.x < tank.x + TANK_SIZE && b.y + BULLET_SIZE > tank.y && b.y < tank.y + TANK_SIZE) {
            return true;
        }
    }
    return false;
}

function isInGrass(x, y, size) {
    for (let g of grasses) {
        if (g.contains(x, y, size)) return true;
    }
    return false;
}

function isInShortGrass(x, y, size) {
    for (let g of shortGrasses) {
        if (g.contains(x, y, size)) return true;
    }
    return false;
}

function gameLoop() {
    ctx.clearRect(0,0,WIDTH,HEIGHT);
    // Draw walls
    for (let w of walls) w.draw();
    // Draw grass (on top of walls)
    for (let g of grasses) g.draw();
    // Draw short grass (on top of grass)
    for (let sg of shortGrasses) sg.draw();
    // Draw breakable walls (above grass)
    for (let bw of breakableWalls) bw.draw();
    // Draw teleporters
    for (let t of teleporters) t.draw();
    // Move tanks
    player1.move();
    player2.move();
    // Dash (E for green, Enter for red)
    if (keys['e']) { player1.dash(); keys['e'] = false; }
    if (keys['Enter']) { player2.dash(); keys['Enter'] = false; }
    // Special attack (Q for green, Right Shift for red)
    if (keys['q']) { player1.specialAttack(); keys['q'] = false; }
    if (keys['Shift']) { player2.specialAttack(); keys['Shift'] = false; }
    // Shoot
    if (keys[player1.controls.shoot]) player1.shoot();
    if (keys[player2.controls.shoot]) player2.shoot();
    // Update bullets (solid + breakable walls)
    function updateBulletsWithBreakables(tank) {
        for (let b of tank.bullets) {
            b.x += b.dx;
            b.y += b.dy;
        }
        tank.bullets = tank.bullets.filter(b => {
            if (b.x < 0 || b.x > WIDTH || b.y < 0 || b.y > HEIGHT) return false;
            // hit solid walls
            for (let w of walls) {
                if (b.x + BULLET_SIZE > w.x && b.x < w.x + WALL_SIZE && b.y + BULLET_SIZE > w.y && b.y < w.y + WALL_SIZE) return false;
            }
            // hit breakable walls
            for (let i = 0; i < breakableWalls.length; i++) {
                const bw = breakableWalls[i];
                if (b.x + BULLET_SIZE > bw.x && b.x < bw.x + WALL_SIZE && b.y + BULLET_SIZE > bw.y && b.y < bw.y + WALL_SIZE) {
                    bw.hp -= 1;
                    if (bw.hp <= 0) breakableWalls.splice(i, 1);
                    return false;
                }
            }
            return true;
        });
        // Update special bullets (ignore walls, same as before)
        for (let b of tank.specialBullets) {
            b.x += b.dx;
            b.y += b.dy;
        }
        tank.specialBullets = tank.specialBullets.filter(b => b.x >= 0 && b.x <= WIDTH && b.y >= 0 && b.y <= HEIGHT);
        if (tank.cooldown > 0) tank.cooldown--;
        if (tank.dashCooldown > 0) tank.dashCooldown--;
        if (tank.specialCooldown > 0) tank.specialCooldown--;
        if (tank.teleportCooldown > 0) tank.teleportCooldown--;
    }

    updateBulletsWithBreakables(player1);
    updateBulletsWithBreakables(player2);
    // Teleporter logic: stand for 3s (180 frames), then teleport to diagonal; 15s cooldown
    const STAND_FRAMES = 60 * 3;
    const TP_COOLDOWN = 60 * 15;
    function processTeleport(tank) {
        // If in cooldown, reset stand time and skip
        if (tank.teleportCooldown > 0) { tank.teleportStandTime = 0; return; }
        // Check overlap with which teleporter
        let onIndex = -1;
        for (let i = 0; i < teleporters.length; i++) {
            if (teleporters[i].contains(tank.x, tank.y, TANK_SIZE)) { onIndex = i; break; }
        }
        if (onIndex === -1) { tank.teleportStandTime = 0; return; }
        // Increment stand time
        tank.teleportStandTime++;
        // Draw progress ring on teleporter while standing
        const t = teleporters[onIndex];
        const cx = t.x + t.size/2, cy = t.y + t.size/2;
        const progress = Math.min(1, tank.teleportStandTime / STAND_FRAMES);
        ctx.save();
        ctx.strokeStyle = tank.color;
        ctx.lineWidth = 5;
        ctx.beginPath();
        ctx.arc(cx, cy, t.size/2 - 10, -Math.PI/2, -Math.PI/2 + progress * Math.PI*2);
        ctx.stroke();
        ctx.restore();
        // Teleport when ready
        if (tank.teleportStandTime >= STAND_FRAMES) {
            const other = teleporters[onIndex ^ 1];
            // place tank centered on other pad, adjusted to keep within bounds
            tank.x = Math.max(0, Math.min(WIDTH - TANK_SIZE, other.x + other.size/2 - TANK_SIZE/2));
            tank.y = Math.max(0, Math.min(HEIGHT - TANK_SIZE, other.y + other.size/2 - TANK_SIZE/2));
            tank.teleportCooldown = TP_COOLDOWN;
            tank.teleportStandTime = 0;
        }
    }
    processTeleport(player1);
    processTeleport(player2);
    // Check hits
    if (checkHit(player1, player2.bullets, player2.specialBullets)) {
        alert('Red wins!'); window.location.reload();
    }
    if (checkHit(player2, player1.bullets, player1.specialBullets)) {
        alert('Green wins!'); window.location.reload();
    }
    // Draw tanks (hide if in grass, half-invisible if in short grass)
    ctx.save();
    if (!isInGrass(player1.x, player1.y, TANK_SIZE)) {
        if (isInShortGrass(player1.x, player1.y, TANK_SIZE)) ctx.globalAlpha = 0.4;
        player1.draw();
        ctx.globalAlpha = 1.0;
    }
    if (!isInGrass(player2.x, player2.y, TANK_SIZE)) {
        if (isInShortGrass(player2.x, player2.y, TANK_SIZE)) ctx.globalAlpha = 0.4;
        player2.draw();
        ctx.globalAlpha = 1.0;
    }
    ctx.restore();
    // Draw bullets (hide if in grass, half-invisible if in short grass)
    ctx.save();
    ctx.fillStyle = '#ff0';
    for (let b of player1.bullets) {
        if (!isInGrass(b.x, b.y, BULLET_SIZE)) {
            if (isInShortGrass(b.x, b.y, BULLET_SIZE)) ctx.globalAlpha = 0.4;
            ctx.fillRect(b.x, b.y, BULLET_SIZE, BULLET_SIZE);
            ctx.globalAlpha = 1.0;
        }
    }
    for (let b of player2.bullets) {
        if (!isInGrass(b.x, b.y, BULLET_SIZE)) {
            if (isInShortGrass(b.x, b.y, BULLET_SIZE)) ctx.globalAlpha = 0.4;
            ctx.fillRect(b.x, b.y, BULLET_SIZE, BULLET_SIZE);
            ctx.globalAlpha = 1.0;
        }
    }
    // Draw special bullets (red for red, green for green)
    ctx.fillStyle = '#0f0';
    for (let b of player1.specialBullets) {
        if (!isInGrass(b.x, b.y, BULLET_SIZE)) {
            if (isInShortGrass(b.x, b.y, BULLET_SIZE)) ctx.globalAlpha = 0.4;
            ctx.fillRect(b.x, b.y, BULLET_SIZE, BULLET_SIZE);
            ctx.globalAlpha = 1.0;
        }
    }
    ctx.fillStyle = '#f00';
    for (let b of player2.specialBullets) {
        if (!isInGrass(b.x, b.y, BULLET_SIZE)) {
            if (isInShortGrass(b.x, b.y, BULLET_SIZE)) ctx.globalAlpha = 0.4;
            ctx.fillRect(b.x, b.y, BULLET_SIZE, BULLET_SIZE);
            ctx.globalAlpha = 1.0;
        }
    }
    ctx.restore();
    // Draw dash and special cooldowns
    ctx.save();
    ctx.font = '20px Arial';
    ctx.fillStyle = '#0f0';
    let cd1 = Math.ceil(player1.dashCooldown/60);
    let scd1 = Math.ceil(player1.specialCooldown/60*10)/10;
    ctx.fillText('Green Dash: ' + (cd1 === 0 ? 'Ready' : cd1 + 's'), 20, 30);
    ctx.fillText('Green Special: ' + (scd1 === 0 ? 'Ready' : scd1 + 's'), 20, 60);
    // Teleport cooldown HUD for green
    let tcd1 = Math.ceil(player1.teleportCooldown/60);
    ctx.fillText('Green Teleport: ' + (tcd1 === 0 ? 'Ready' : tcd1 + 's'), 20, 90);
    ctx.fillStyle = '#f00';
    let cd2 = Math.ceil(player2.dashCooldown/60);
    let scd2 = Math.ceil(player2.specialCooldown/60*10)/10;
    ctx.fillText('Red Dash: ' + (cd2 === 0 ? 'Ready' : cd2 + 's'), WIDTH-180, 30);
    ctx.fillText('Red Special: ' + (scd2 === 0 ? 'Ready' : scd2 + 's'), WIDTH-180, 60);
    // Teleport cooldown HUD for red
    let tcd2 = Math.ceil(player2.teleportCooldown/60);
    ctx.fillText('Red Teleport: ' + (tcd2 === 0 ? 'Ready' : tcd2 + 's'), WIDTH-180, 90);
    ctx.restore();
    requestAnimationFrame(gameLoop);
}
gameLoop();
