const canvas = document.getElementById('gameCanvas');
const ctx = canvas.getContext('2d');
const scoreText = document.getElementById('scoreText');
const startButton = document.getElementById('startButton');

// 获取选项元素
const areaSizeSelect = document.getElementById('areaSize');
const blockSizeSelect = document.getElementById('blockSize');
const gameSpeedSelect = document.getElementById('gameSpeed');
const foodCountSelect = document.getElementById('foodCount');
const historyList = document.getElementById('historyList');
const leaderboardList = document.getElementById('leaderboardList');
const multiplierText = document.getElementById('multiplierText');

// AI机器人相关元素
const aiEnabledCheckbox = document.getElementById('aiEnabled');
const aiBotSelect = document.getElementById('aiBotSelect');
const reloadBotsButton = document.getElementById('reloadBotsButton');
const aiStatusText = document.getElementById('aiStatusText');

// 调试相关元素
const debugEnabledCheckbox = document.getElementById('debugEnabled');
const debugPanel = document.getElementById('debugPanel');
const gameStateText = document.getElementById('gameStateText');
const aiDecisionText = document.getElementById('aiDecisionText');

// 游戏配置
let gameConfig = {
    areaSize: 'medium',
    blockSize: 'medium',
    gameSpeed: 'medium',
    foodCount: 1
};

// 游戏参数
let canvasSize = 800;
let gridSize = 20;
let tileCount = canvasSize / gridSize;
let pixelSpeed = 200; // 像素/秒
let lastUpdateTime = 0;
let score = 0;

// 蛇的初始位置和速度
let snake = [
    { x: 5, y: 5 }
];
let velocityX = 0;
let velocityY = 0;

// 待处理的方向（用于防止快速按键导致的问题）
let pendingVelocityX = 0;
let pendingVelocityY = 0;

// 按键缓存数组，存储待处理的按键
let keyQueue = [];

// 食物数组
let foods = [];

// 游戏状态
let gameRunning = false;
let gamePaused = false;

// AI机器人相关变量
let aiEnabled = false;
let currentBot = null;
let availableBots = [];

// 调试相关变量
let debugEnabled = false;
let lastGameState = null;
let aiCallCount = 0;
let aiSuccessCount = 0;
let aiFailureCount = 0;

// AI机器人实现
const AIBots = {
    // 随机机器人 - 随机选择有效移动
    '随机机器人': {
        name: '随机机器人',
        getNextMove: function(gameState) {
            const validMoves = getValidMoves(gameState);
            if (validMoves.length > 0) {
                return validMoves[Math.floor(Math.random() * validMoves.length)];
            }
            return gameState.currentDirection;
        }
    },

    // 贪吃蛇机器人 - 总是朝着最近的食物移动
    '贪吃蛇机器人': {
        name: '贪吃蛇机器人',
        getNextMove: function(gameState) {
            const head = gameState.snake[0];
            const foods = gameState.foods;
            
            if (foods.length === 0) {
                return gameState.currentDirection;
            }
            
            // 找到最近的食物
            let closestFood = foods[0];
            let minDistance = Infinity;
            
            for (const food of foods) {
                const distance = Math.abs(food.x - head.x) + Math.abs(food.y - head.y);
                if (distance < minDistance) {
                    minDistance = distance;
                    closestFood = food;
                }
            }
            
            // 计算朝向食物的方向
            const dx = closestFood.x - head.x;
            const dy = closestFood.y - head.y;
            
            const validMoves = getValidMoves(gameState);
            if (validMoves.length === 0) {
                return gameState.currentDirection;
            }
            
            // 优先选择朝向食物的方向
            if (dx > 0 && validMoves.includes('right')) return 'right';
            if (dx < 0 && validMoves.includes('left')) return 'left';
            if (dy > 0 && validMoves.includes('down')) return 'down';
            if (dy < 0 && validMoves.includes('up')) return 'up';
            
            // 如果没有直接朝向食物的移动，选择第一个有效移动
            return validMoves[0];
        }
    },

    // 智能机器人 - 使用A*算法寻找最短路径
    '智能机器人': {
        name: '智能机器人',
        getNextMove: function(gameState) {
            const head = gameState.snake[0];
            const foods = gameState.foods;
            
            if (foods.length === 0) {
                return gameState.currentDirection;
            }
            
            // 找到最近的食物
            let closestFood = foods[0];
            let minDistance = Infinity;
            
            for (const food of foods) {
                const distance = Math.abs(food.x - head.x) + Math.abs(food.y - head.y);
                if (distance < minDistance) {
                    minDistance = distance;
                    closestFood = food;
                }
            }
            
            // 使用A*算法寻找路径
            const path = this.findPath(gameState, head, closestFood);
            
            if (path && path.length > 1) {
                const nextPos = path[1];
                const dx = nextPos.x - head.x;
                const dy = nextPos.y - head.y;
                
                if (dx > 0) return 'right';
                if (dx < 0) return 'left';
                if (dy > 0) return 'down';
                if (dy < 0) return 'up';
            }
            
            // 如果找不到路径，使用贪吃蛇策略
            const validMoves = getValidMoves(gameState);
            if (validMoves.length === 0) {
                return gameState.currentDirection;
            }
            
            const dx = closestFood.x - head.x;
            const dy = closestFood.y - head.y;
            
            if (dx > 0 && validMoves.includes('right')) return 'right';
            if (dx < 0 && validMoves.includes('left')) return 'left';
            if (dy > 0 && validMoves.includes('down')) return 'down';
            if (dy < 0 && validMoves.includes('up')) return 'up';
            
            return validMoves[0];
        },
        
        findPath: function(gameState, start, goal) {
            const openSet = [start];
            const cameFrom = new Map();
            const gScore = new Map();
            const fScore = new Map();
            
            gScore.set(`${start.x},${start.y}`, 0);
            fScore.set(`${start.x},${start.y}`, this.heuristic(start, goal));
            
            while (openSet.length > 0) {
                // 找到fScore最小的节点
                let current = openSet[0];
                let currentIndex = 0;
                
                for (let i = 1; i < openSet.length; i++) {
                    const currentKey = `${current.x},${current.y}`;
                    const testKey = `${openSet[i].x},${openSet[i].y}`;
                    if (fScore.get(testKey) < fScore.get(currentKey)) {
                        current = openSet[i];
                        currentIndex = i;
                    }
                }
                
                if (current.x === goal.x && current.y === goal.y) {
                    // 重建路径
                    const path = [];
                    while (current) {
                        path.unshift(current);
                        current = cameFrom.get(`${current.x},${current.y}`);
                    }
                    return path;
                }
                
                openSet.splice(currentIndex, 1);
                
                // 检查四个方向
                const directions = [
                    { dx: 0, dy: -1 }, { dx: 0, dy: 1 },
                    { dx: -1, dy: 0 }, { dx: 1, dy: 0 }
                ];
                
                for (const dir of directions) {
                    const neighbor = { x: current.x + dir.dx, y: current.y + dir.dy };
                    
                    // 检查边界
                    if (neighbor.x < 0 || neighbor.x >= gameState.gridSize ||
                        neighbor.y < 0 || neighbor.y >= gameState.gridSize) {
                        continue;
                    }
                    
                    // 检查是否撞到蛇身
                    let collision = false;
                    for (const part of gameState.snake) {
                        if (part.x === neighbor.x && part.y === neighbor.y) {
                            collision = true;
                            break;
                        }
                    }
                    if (collision) continue;
                    
                    const neighborKey = `${neighbor.x},${neighbor.y}`;
                    const currentKey = `${current.x},${current.y}`;
                    const tentativeGScore = gScore.get(currentKey) + 1;
                    
                    if (!gScore.has(neighborKey) || tentativeGScore < gScore.get(neighborKey)) {
                        cameFrom.set(neighborKey, current);
                        gScore.set(neighborKey, tentativeGScore);
                        fScore.set(neighborKey, tentativeGScore + this.heuristic(neighbor, goal));
                        
                        if (!openSet.some(pos => pos.x === neighbor.x && pos.y === neighbor.y)) {
                            openSet.push(neighbor);
                        }
                    }
                }
            }
            
            return null; // 没有找到路径
        },
        
        heuristic: function(a, b) {
            return Math.abs(a.x - b.x) + Math.abs(a.y - b.y);
        }
    },

    // 保守机器人 - 优先避免危险，保持安全距离
    '保守机器人': {
        name: '保守机器人',
        getNextMove: function(gameState) {
            const head = gameState.snake[0];
            const validMoves = getValidMoves(gameState);
            
            if (validMoves.length === 0) {
                return gameState.currentDirection;
            }
            
            // 评估每个移动的安全性
            const moveScores = {};
            
            for (const move of validMoves) {
                let score = 0;
                const newHead = this.getNewHead(head, move);
                
                // 检查是否接近边界
                const borderDistance = Math.min(
                    newHead.x, newHead.y,
                    gameState.gridSize - 1 - newHead.x,
                    gameState.gridSize - 1 - newHead.y
                );
                score += borderDistance * 2;
                
                // 检查是否接近蛇身
                let minSnakeDistance = Infinity;
                for (let i = 1; i < gameState.snake.length; i++) {
                    const distance = Math.abs(newHead.x - gameState.snake[i].x) + 
                                   Math.abs(newHead.y - gameState.snake[i].y);
                    minSnakeDistance = Math.min(minSnakeDistance, distance);
                }
                score += minSnakeDistance * 3;
                
                // 检查是否有食物在附近
                for (const food of gameState.foods) {
                    const distance = Math.abs(newHead.x - food.x) + Math.abs(newHead.y - food.y);
                    if (distance <= 2) {
                        score += 5;
                    }
                }
                
                moveScores[move] = score;
            }
            
            // 选择得分最高的移动
            let bestMove = validMoves[0];
            let bestScore = moveScores[bestMove];
            
            for (const move of validMoves) {
                if (moveScores[move] > bestScore) {
                    bestMove = move;
                    bestScore = moveScores[move];
                }
            }
            
            return bestMove;
        },
        
        getNewHead: function(head, move) {
            switch(move) {
                case 'up': return { x: head.x, y: head.y - 1 };
                case 'down': return { x: head.x, y: head.y + 1 };
                case 'left': return { x: head.x - 1, y: head.y };
                case 'right': return { x: head.x + 1, y: head.y };
                default: return head;
            }
        }
    },

    // 激进机器人 - 总是追求最高分，冒险性强
    '激进机器人': {
        name: '激进机器人',
        getNextMove: function(gameState) {
            const head = gameState.snake[0];
            const foods = gameState.foods;
            
            if (foods.length === 0) {
                return gameState.currentDirection;
            }
            
            // 找到最近的食物
            let closestFood = foods[0];
            let minDistance = Infinity;
            
            for (const food of foods) {
                const distance = Math.abs(food.x - head.x) + Math.abs(food.y - head.y);
                if (distance < minDistance) {
                    minDistance = distance;
                    closestFood = food;
                }
            }
            
            // 尝试直接路径到食物
            const directPath = this.findDirectPath(gameState, head, closestFood);
            if (directPath) {
                return directPath;
            }
            
            // 如果直接路径不可行，使用智能路径
            const path = this.findPath(gameState, head, closestFood);
            if (path && path.length > 1) {
                const nextPos = path[1];
                const dx = nextPos.x - head.x;
                const dy = nextPos.y - head.y;
                
                if (dx > 0) return 'right';
                if (dx < 0) return 'left';
                if (dy > 0) return 'down';
                if (dy < 0) return 'up';
            }
            
            // 最后使用贪吃蛇策略
            const validMoves = getValidMoves(gameState);
            if (validMoves.length === 0) {
                return gameState.currentDirection;
            }
            
            const dx = closestFood.x - head.x;
            const dy = closestFood.y - head.y;
            
            if (dx > 0 && validMoves.includes('right')) return 'right';
            if (dx < 0 && validMoves.includes('left')) return 'left';
            if (dy > 0 && validMoves.includes('down')) return 'down';
            if (dy < 0 && validMoves.includes('up')) return 'up';
            
            return validMoves[0];
        },
        
        findDirectPath: function(gameState, start, goal) {
            const dx = goal.x - start.x;
            const dy = goal.y - start.y;
            
            // 尝试水平移动
            if (dx !== 0) {
                const horizontalMove = dx > 0 ? 'right' : 'left';
                if (this.isValidMove(gameState, start, horizontalMove)) {
                    return horizontalMove;
                }
            }
            
            // 尝试垂直移动
            if (dy !== 0) {
                const verticalMove = dy > 0 ? 'down' : 'up';
                if (this.isValidMove(gameState, start, verticalMove)) {
                    return verticalMove;
                }
            }
            
            return null;
        },
        
        isValidMove: function(gameState, head, move) {
            const newHead = this.getNewHead(head, move);
            
            // 检查边界
            if (newHead.x < 0 || newHead.x >= gameState.gridSize ||
                newHead.y < 0 || newHead.y >= gameState.gridSize) {
                return false;
            }
            
            // 检查蛇身碰撞
            for (const part of gameState.snake) {
                if (part.x === newHead.x && part.y === newHead.y) {
                    return false;
                }
            }
            
            return true;
        },
        
        getNewHead: function(head, move) {
            switch(move) {
                case 'up': return { x: head.x, y: head.y - 1 };
                case 'down': return { x: head.x, y: head.y + 1 };
                case 'left': return { x: head.x - 1, y: head.y };
                case 'right': return { x: head.x + 1, y: head.y };
                default: return head;
            }
        },
        
        findPath: function(gameState, start, goal) {
            // 简化的路径查找算法
            const queue = [{ pos: start, path: [] }];
            const visited = new Set();
            
            while (queue.length > 0) {
                const current = queue.shift();
                const key = `${current.pos.x},${current.pos.y}`;
                
                if (visited.has(key)) continue;
                visited.add(key);
                
                if (current.pos.x === goal.x && current.pos.y === goal.y) {
                    return current.path;
                }
                
                const directions = [
                    { dx: 0, dy: -1, move: 'up' },
                    { dx: 0, dy: 1, move: 'down' },
                    { dx: -1, dy: 0, move: 'left' },
                    { dx: 1, dy: 0, move: 'right' }
                ];
                
                for (const dir of directions) {
                    const newPos = {
                        x: current.pos.x + dir.dx,
                        y: current.pos.y + dir.dy
                    };
                    
                    if (this.isValidMove(gameState, current.pos, dir.move)) {
                        queue.push({
                            pos: newPos,
                            path: [...current.path, dir.move]
                        });
                    }
                }
            }
            
            return null;
        }
    }
};

// 历史分数
let historyScores = JSON.parse(localStorage.getItem('snakeHistory') || '[]');

// 排行榜当前筛选
let currentLeaderboardPeriod = 'all';

// 初始化
initGame();
loadHistory();
loadLeaderboard();
initAI();

// 事件监听
startButton.addEventListener('click', startGame);
document.addEventListener('keydown', handleKeyPress);

// AI机器人事件监听
aiEnabledCheckbox.addEventListener('change', toggleAI);
aiBotSelect.addEventListener('change', selectBot);
reloadBotsButton.addEventListener('click', reloadBots);

// 调试事件监听
debugEnabledCheckbox.addEventListener('change', toggleDebug);

// 选项变化监听
areaSizeSelect.addEventListener('change', updateConfig);
blockSizeSelect.addEventListener('change', updateConfig);
gameSpeedSelect.addEventListener('change', updateConfig);
foodCountSelect.addEventListener('change', updateConfig);

// 排行榜标签切换监听
document.querySelectorAll('.tab-button').forEach(button => {
    button.addEventListener('click', function() {
        // 移除所有活动状态
        document.querySelectorAll('.tab-button').forEach(btn => btn.classList.remove('active'));
        // 添加当前活动状态
        this.classList.add('active');
        // 更新筛选条件
        currentLeaderboardPeriod = this.dataset.period;
        loadLeaderboard();
    });
});

function initGame() {
    updateConfig();
    updateCanvas();
    drawGame();
}

function updateConfig() {
    gameConfig.areaSize = areaSizeSelect.value;
    gameConfig.blockSize = blockSizeSelect.value;
    gameConfig.gameSpeed = gameSpeedSelect.value;
    gameConfig.foodCount = parseInt(foodCountSelect.value);
    
    // 更新画布大小
    switch(gameConfig.areaSize) {
        case 'small':
            canvasSize = 400;
            break;
        case 'medium':
            canvasSize = 800;
            break;
        case 'large':
            canvasSize = 1100;
            break;
    }
    
    // 更新方块大小
    switch(gameConfig.blockSize) {
        case 'small':
            gridSize = 15;
            break;
        case 'medium':
            gridSize = 20;
            break;
        case 'large':
            gridSize = 25;
            break;
        case 'huge':
            gridSize = 50;
            break;
    }
    
    // 更新游戏速度（像素/秒）
    switch(gameConfig.gameSpeed) {
        case 'slow':
            pixelSpeed = 100; // 慢速：100像素/秒
            break;
        case 'medium':
            pixelSpeed = 200; // 中速：200像素/秒
            break;
        case 'fast':
            pixelSpeed = 400; // 快速：400像素/秒
            break;
        case 'huge':
            pixelSpeed = 800; // 巨大：800像素/秒
            break;
        case 'superfast':
            pixelSpeed = 20000; // 超快：20000像素/秒
            break;
    }
    
    // 计算tileCount，如果gridSize是15则使用调整后的canvas大小
    if (gridSize === 15) {
        const targetSize = Math.round(canvasSize / 15) * 15;
        tileCount = targetSize / gridSize;
    } else {
        tileCount = canvasSize / gridSize;
    }
    updateCanvas();
    updateMultiplierDisplay();
}

function updateCanvas() {
    // 如果gridSize是15，调整canvas大小到最接近的15的倍数
    if (gridSize === 15) {
        const targetSize = Math.round(canvasSize / 15) * 15;
        canvas.width = targetSize;
        canvas.height = targetSize;
        // 更新tileCount以匹配新的canvas大小
        tileCount = targetSize / gridSize;
    } else {
        canvas.width = canvasSize;
        canvas.height = canvasSize;
    }
    drawGame();
}

function startGame() {
    if (gameRunning) return;
    
    gameRunning = true;
    startButton.disabled = true;
    startButton.textContent = '游戏中...';
    
    // 重置游戏状态
    snake = [{ x: Math.floor(tileCount / 2), y: Math.floor(tileCount / 2) }];
    score = 0;
    scoreText.textContent = score;
    velocityX = 1;
    velocityY = 0;
    pendingVelocityX = 1;
    pendingVelocityY = 0;
    keyQueue = [];
    
    // 重置AI统计信息和缓存
    aiCallCount = 0;
    aiSuccessCount = 0;
    aiFailureCount = 0;
    lastAIMove = null;
    lastAIGameState = null;
    
    // 生成食物
    generateFoods();
    
    // 清除游戏结束界面
    drawGame();
    
    // 重置时间
    lastUpdateTime = 0;
    gameLoop(performance.now());
}

function gameLoop(currentTime) {
    if (!gameRunning) return;
    
    // 如果游戏暂停，只重绘界面
    if (gamePaused) {
        drawGame();
        requestAnimationFrame(gameLoop);
        return;
    }
    
    // 计算时间差
    if (lastUpdateTime === 0) {
        lastUpdateTime = currentTime;
    }
    
    const deltaTime = currentTime - lastUpdateTime;
    const moveInterval = (gridSize * 1000) / pixelSpeed;
    
    if (deltaTime >= moveInterval) {
        updateGame();
        lastUpdateTime = currentTime;
    }
    
    drawGame();
    
    if (!gameRunning) {
        showGameOverScreen();
    }
    
    requestAnimationFrame(gameLoop);
}

function updateGame() {
    // 同步调用AI更新
    updateAI();
    
    // 应用待处理的方向
    velocityX = pendingVelocityX;
    velocityY = pendingVelocityY;
    
    // 移动蛇
    const head = { x: snake[0].x + velocityX, y: snake[0].y + velocityY };
    snake.unshift(head);

    // 检查是否吃到食物
    let foodEaten = false;
    for (let i = 0; i < foods.length; i++) {
        if (head.x === foods[i].x && head.y === foods[i].y) {
            score += calculateScore();
            scoreText.textContent = score;
            foods.splice(i, 1);
            foodEaten = true;
            break;
        }
    }
    
    if (!foodEaten) {
        snake.pop();
    }
    
    // 如果食物数量不足，生成新食物
    while (foods.length < gameConfig.foodCount) {
        generateFood();
    }

    // 处理缓存队列中的下一个按键
    if (keyQueue.length > 0) {
        const nextKey = keyQueue.shift();
        processKey(nextKey);
    }

    // 检查游戏结束条件
    if (isGameOver()) {
        endGame();
    }
}

function drawGame() {
    // 清空画布
    ctx.fillStyle = 'white';
    ctx.fillRect(0, 0, canvas.width, canvas.height);

    // 绘制网格
    ctx.strokeStyle = '#f0f0f0';
    ctx.lineWidth = 1;
    for (let i = 0; i <= tileCount; i++) {
        ctx.beginPath();
        ctx.moveTo(i * gridSize, 0);
        ctx.lineTo(i * gridSize, canvas.height);
        ctx.stroke();
        
        ctx.beginPath();
        ctx.moveTo(0, i * gridSize);
        ctx.lineTo(canvas.width, i * gridSize);
        ctx.stroke();
    }

    // 绘制蛇
    ctx.fillStyle = '#4CAF50';
    snake.forEach((part, index) => {
        if (index === 0) {
            // 蛇头
            ctx.fillStyle = '#2E7D32';
        } else {
            ctx.fillStyle = '#4CAF50';
        }
        ctx.fillRect(part.x * gridSize + 1, part.y * gridSize + 1, gridSize - 2, gridSize - 2);
    });

    // 绘制食物
    ctx.fillStyle = '#FF5722';
    foods.forEach(food => {
        ctx.fillRect(food.x * gridSize + 1, food.y * gridSize + 1, gridSize - 2, gridSize - 2);
    });
}

function showGameOverScreen() {
    // 绘制半透明背景
    ctx.fillStyle = 'rgba(0, 0, 0, 0.7)';
    ctx.fillRect(0, 0, canvas.width, canvas.height);
    
    // 绘制游戏结束面板
    const panelWidth = Math.min(400, canvas.width * 0.8);
    const panelHeight = 200;
    const panelX = (canvas.width - panelWidth) / 2;
    const panelY = (canvas.height - panelHeight) / 2;
    
    // 面板背景
    ctx.fillStyle = 'white';
    ctx.fillRect(panelX, panelY, panelWidth, panelHeight);
    
    // 面板边框
    ctx.strokeStyle = '#4CAF50';
    ctx.lineWidth = 3;
    ctx.strokeRect(panelX, panelY, panelWidth, panelHeight);
    
    // 游戏结束文字
    ctx.fillStyle = '#FF5722';
    ctx.font = `bold ${Math.min(32, canvas.width / 20)}px Arial`;
    ctx.textAlign = 'center';
    ctx.fillText('游戏结束', canvas.width / 2, panelY + 50);
    
    // 分数文字
    ctx.fillStyle = '#333';
    ctx.font = `${Math.min(24, canvas.width / 25)}px Arial`;
    ctx.fillText(`最终得分: ${score}`, canvas.width / 2, panelY + 90);
    
    // 提示文字
    ctx.fillStyle = '#666';
    ctx.font = `${Math.min(16, canvas.width / 30)}px Arial`;
    ctx.fillText('点击"重新开始"按钮继续游戏', canvas.width / 2, panelY + 130);
}

function generateFoods() {
    foods = [];
    for (let i = 0; i < gameConfig.foodCount; i++) {
        generateFood();
    }
}

function generateFood() {
    let newFood;
    do {
        newFood = {
            x: Math.floor(Math.random() * tileCount),
            y: Math.floor(Math.random() * tileCount)
        };
    } while (isPositionOccupied(newFood.x, newFood.y));
    
    foods.push(newFood);
}

function isPositionOccupied(x, y) {
    // 检查是否与蛇身重叠
    for (let part of snake) {
        if (part.x === x && part.y === y) {
            return true;
        }
    }
    
    // 检查是否与现有食物重叠
    for (let food of foods) {
        if (food.x === x && food.y === y) {
            return true;
        }
    }
    
    return false;
}

function handleKeyPress(event) {
    // 处理回车键暂停/继续
    if (event.key === 'Enter') {
        event.preventDefault();
        togglePause();
        return;
    }
    
    // 如果游戏暂停或未运行，不处理方向键
    if (!gameRunning || gamePaused) return;
    
    let key = null;
    
    switch(event.key) {
        case 'ArrowUp':
            key = 'up';
            break;
        case 'ArrowDown':
            key = 'down';
            break;
        case 'ArrowLeft':
            key = 'left';
            break;
        case 'ArrowRight':
            key = 'right';
            break;
    }
    
    // 如果按键有效且缓存队列为空，直接处理
    if (key && keyQueue.length === 0) {
        processKey(key);
    } else if (key) {
        // 否则添加到缓存队列
        keyQueue.push(key);
    }
}

function processKey(key) {
    switch(key) {
        case 'up':
            // 只有当当前方向不是向下时，才允许向上
            if (velocityY !== 1) {
                pendingVelocityX = 0;
                pendingVelocityY = -1;
            }
            break;
        case 'down':
            // 只有当当前方向不是向上时，才允许向下
            if (velocityY !== -1) {
                pendingVelocityX = 0;
                pendingVelocityY = 1;
            }
            break;
        case 'left':
            // 只有当当前方向不是向右时，才允许向左
            if (velocityX !== 1) {
                pendingVelocityX = -1;
                pendingVelocityY = 0;
            }
            break;
        case 'right':
            // 只有当当前方向不是向左时，才允许向右
            if (velocityX !== -1) {
                pendingVelocityX = 1;
                pendingVelocityY = 0;
            }
            break;
    }
}

function isGameOver() {
    // 撞墙
    if (snake[0].x < 0 || snake[0].x >= tileCount || 
        snake[0].y < 0 || snake[0].y >= tileCount) {
        return true;
    }

    // 撞到自己
    for (let i = 1; i < snake.length; i++) {
        if (snake[i].x === snake[0].x && snake[i].y === snake[0].y) {
            return true;
        }
    }
    return false;
}

function endGame() {
    gameRunning = false;
    startButton.disabled = false;
    startButton.textContent = '重新开始';
    
    // 保存历史分数
    const historyItem = {
        score: score,
        date: new Date().toLocaleString('zh-CN'),
        settings: {
            areaSize: gameConfig.areaSize,
            blockSize: gameConfig.blockSize,
            gameSpeed: gameConfig.gameSpeed,
            foodCount: gameConfig.foodCount
        }
    };
    
    historyScores.unshift(historyItem);
    
    // 只保留最近20条记录
    if (historyScores.length > 20) {
        historyScores = historyScores.slice(0, 20);
    }
    
    localStorage.setItem('snakeHistory', JSON.stringify(historyScores));
    loadHistory();
    loadLeaderboard();
    
    // 在画布上显示游戏结束信息
    showGameOverScreen();
}

function loadHistory() {
    historyList.innerHTML = '';
    
    if (historyScores.length === 0) {
        historyList.innerHTML = '<p style="text-align: center; color: #666;">暂无历史记录</p>';
        return;
    }
    
    historyScores.forEach((item, index) => {
        const historyItem = document.createElement('div');
        historyItem.className = 'history-item';
        
        const settingsText = `区域:${getAreaSizeText(item.settings.areaSize)} | 方块:${getBlockSizeText(item.settings.blockSize)} | 速度:${getSpeedText(item.settings.gameSpeed)} | 食物:${item.settings.foodCount}个`;
        
        historyItem.innerHTML = `
            <div class="score">${item.score}分</div>
            <div class="date">${item.date}</div>
            <div class="settings">${settingsText}</div>
        `;
        
        historyList.appendChild(historyItem);
    });
}

function getAreaSizeText(size) {
    switch(size) {
        case 'small': return '小';
        case 'medium': return '中';
        case 'large': return '大';
        default: return size;
    }
}

function getBlockSizeText(size) {
    switch(size) {
        case 'small': return '小';
        case 'medium': return '中';
        case 'large': return '大';
        case 'huge': return '巨大';
        default: return size;
    }
}

function getSpeedText(speed) {
    switch(speed) {
        case 'slow': return '慢';
        case 'medium': return '中';
        case 'fast': return '快';
        case 'huge': return '巨大';
        case 'superfast': return '超快';
        default: return speed;
    }
}

function calculateMultiplier() {
    let multiplier = 1.0;
    
    // 食物数量折扣 - 食物越多越容易，分数打折
    switch(gameConfig.foodCount) {
        case 1:
            multiplier *= 1.0; // 标准分数
            break;
        case 2:
            multiplier *= 0.6; // 8折
            break;
        case 3:
            multiplier *= 0.3; // 6折
            break;
    }
    
    // 速度折扣 - 速度越慢越容易，分数打折
    switch(gameConfig.gameSpeed) {
        case 'slow':
            multiplier *= 0.5; // 7折
            break;
        case 'medium':
            multiplier *= 1.0; // 标准分数
            break;
        case 'fast':
            multiplier *= 2.0; // 加成30%
            break;
        case 'huge':
            multiplier *= 4.0; // 加成50%
            break;
        case 'superfast':
            multiplier *= 20.0; // 加成50%
            break;
    }
    
    // 区域大小折扣 - 区域越大越容易，分数打折
    switch(gameConfig.areaSize) {
        case 'small':
            multiplier *= 1.5; // 加成20%
            break;
        case 'medium':
            multiplier *= 1.0; // 标准分数
            break;
        case 'large':
            multiplier *= 0.5; // 8折
            break;
    }
    
    // 方块大小影响 - 方块越大越容易操作，分数打折
    switch(gameConfig.blockSize) {
        case 'small':
            multiplier *= 1.1; // 加成10%
            break;
        case 'medium':
            multiplier *= 1.0; // 标准分数
            break;
        case 'large':
            multiplier *= 0.9; // 9折
            break;
        case 'huge':
            multiplier *= 0.8; // 8折
            break;
    }
    
    return multiplier;
}

function calculateScore() {
    let baseScore = 10;
    let multiplier = calculateMultiplier();
    
    // 计算最终分数，四舍五入到整数
    return Math.round(baseScore * multiplier);
}

function updateMultiplierDisplay() {
    let multiplier = calculateMultiplier();
    multiplierText.textContent = multiplier.toFixed(1) + 'x';
    
    // 根据倍率设置颜色
    if (multiplier > 1.0) {
        multiplierText.style.color = '#3498db'; // 蓝色表示加成
    } else if (multiplier < 1.0) {
        multiplierText.style.color = '#e74c3c'; // 红色表示折扣
    } else {
        multiplierText.style.color = '#7f8c8d'; // 灰色表示标准
    }
}

function loadLeaderboard() {
    leaderboardList.innerHTML = '';
    
    // 根据筛选条件获取分数
    let filteredScores = [];
    const now = new Date();
    const today = new Date(now.getFullYear(), now.getMonth(), now.getDate());
    const weekStart = new Date(today.getTime() - (today.getDay() * 24 * 60 * 60 * 1000));
    
    switch(currentLeaderboardPeriod) {
        case 'today':
            filteredScores = historyScores.filter(item => {
                const itemDate = new Date(item.date);
                return itemDate >= today;
            });
            break;
        case 'week':
            filteredScores = historyScores.filter(item => {
                const itemDate = new Date(item.date);
                return itemDate >= weekStart;
            });
            break;
        default: // 'all'
            filteredScores = [...historyScores];
            break;
    }
    
    // 按分数排序
    filteredScores.sort((a, b) => b.score - a.score);
    
    if (filteredScores.length === 0) {
        leaderboardList.innerHTML = '<p style="text-align: center; color: #666;">暂无记录</p>';
        return;
    }
    
    // 显示前10名
    filteredScores.slice(0, 10).forEach((item, index) => {
        const leaderboardItem = document.createElement('div');
        leaderboardItem.className = `leaderboard-item rank-${index + 1}`;
        
        const settingsText = `区域:${getAreaSizeText(item.settings.areaSize)} | 方块:${getBlockSizeText(item.settings.blockSize)} | 速度:${getSpeedText(item.settings.gameSpeed)} | 食物:${item.settings.foodCount}个`;
        
        leaderboardItem.innerHTML = `
            <div class="rank">#${index + 1}</div>
            <div class="score">${item.score}分</div>
            <div class="date">${item.date}</div>
            <div class="settings">${settingsText}</div>
        `;
        
        leaderboardList.appendChild(leaderboardItem);
    });
}

// AI机器人相关函数
function toggleAI() {
    aiEnabled = aiEnabledCheckbox.checked;
    updateAIStatus();
    
    if (aiEnabled && !currentBot) {
        // 如果启用AI但没有选择机器人，选择第一个可用的
        if (availableBots.length > 0) {
            aiBotSelect.value = availableBots[0];
            selectBot();
        }
    }
}

function selectBot() {
    const selectedBotName = aiBotSelect.value;
    if (selectedBotName && aiEnabled) {
        try {
            // 从本地AI机器人中选择
            if (AIBots[selectedBotName]) {
                currentBot = AIBots[selectedBotName];
                updateAIStatus();
                console.log('选择机器人成功:', selectedBotName);
            } else {
                console.error('机器人不存在:', selectedBotName);
                currentBot = null;
                updateAIStatus();
            }
        } catch (error) {
            console.error('选择机器人失败:', error);
            currentBot = null;
            updateAIStatus();
        }
    } else {
        currentBot = null;
        updateAIStatus();
    }
}

function reloadBots() {
    // 重新加载本地机器人列表
    availableBots = Object.keys(AIBots);
    updateBotSelect();
    updateAIStatus();
    console.log('机器人重新加载成功，可用机器人:', availableBots);
}

function updateBotSelect() {
    aiBotSelect.innerHTML = '<option value="">请选择机器人</option>';
    availableBots.forEach(botName => {
        const option = document.createElement('option');
        option.value = botName;
        option.textContent = botName;
        aiBotSelect.appendChild(option);
    });
}

function updateAIStatus() {
    const statusElement = document.querySelector('.ai-status');
    if (aiEnabled && currentBot) {
        aiStatusText.textContent = `AI已启用: ${currentBot.name}`;
        statusElement.classList.add('active');
    } else if (aiEnabled) {
        aiStatusText.textContent = 'AI已启用，但未选择机器人';
        statusElement.classList.remove('active');
    } else {
        aiStatusText.textContent = 'AI未启用';
        statusElement.classList.remove('active');
    }
}

function getValidMoves(gameState) {
    const moves = [];
    const head = gameState.snake[0];
    const gridSize = gameState.gridSize;
    const currentDirection = gameState.currentDirection;
    
    // 检查四个方向
    const directions = [
        { name: 'up', dx: 0, dy: -1 },
        { name: 'down', dx: 0, dy: 1 },
        { name: 'left', dx: -1, dy: 0 },
        { name: 'right', dx: 1, dy: 0 }
    ];
    
    for (const dir of directions) {
        const newX = head.x + dir.dx;
        const newY = head.y + dir.dy;
        
        // 检查是否撞墙
        if (newX < 0 || newX >= gridSize || newY < 0 || newY >= gridSize) {
            continue;
        }
        
        // 检查是否撞到自己
        let collision = false;
        for (const part of gameState.snake) {
            if (part.x === newX && part.y === newY) {
                collision = true;
                break;
            }
        }
        if (collision) continue;
        
        // 检查是否反向移动
        if ((dir.name === 'up' && currentDirection === 'down') ||
            (dir.name === 'down' && currentDirection === 'up') ||
            (dir.name === 'left' && currentDirection === 'right') ||
            (dir.name === 'right' && currentDirection === 'left')) {
            continue;
        }
        
        moves.push(dir.name);
    }
    
    return moves;
}

// 初始化AI系统
function initAI() {
    try {
        // 从本地AI机器人对象中获取可用机器人列表
        availableBots = Object.keys(AIBots);
        updateBotSelect();
        updateAIStatus();
        console.log('AI系统初始化成功，可用机器人:', availableBots);
    } catch (error) {
        console.error('AI系统初始化失败:', error);
        // 使用备用数据
        availableBots = ['随机机器人', '贪吃蛇机器人'];
        updateBotSelect();
        updateAIStatus();
    }
}

// AI相关变量
let lastAIMove = null;
let lastAIGameState = null;

// 使用同步调用本地AI机器人
function updateAI() {
    if (aiEnabled && currentBot && gameRunning) {
        const gameState = {
            snake: snake.map(part => ({ x: part.x, y: part.y })),
            foods: foods.map(food => ({ x: food.x, y: food.y })),
            gridSize: tileCount,
            currentDirection: getCurrentDirection()
        };
        
        // 保存游戏状态用于调试
        lastGameState = gameState;
        
        // 更新调试信息
        if (debugEnabled) {
            updateDebugInfo(gameState);
        }
        
        try {
            // 增加调用计数
            aiCallCount++;
            
            // 同步调用本地AI机器人
            const move = currentBot.getNextMove(gameState);
            
            // 增加成功计数
            aiSuccessCount++;
            
            // 更新AI决策信息
            if (debugEnabled) {
                updateAIDecision(move);
            }
            
            processAIMove(move);
            
        } catch (error) {
            console.error('AI更新失败:', error);
            
            // 增加失败计数
            aiFailureCount++;
            
            // 如果AI失败，使用随机移动
            const validMoves = getValidMoves(gameState);
            const fallbackMove = validMoves.length > 0 ? 
                validMoves[Math.floor(Math.random() * validMoves.length)] : 
                gameState.currentDirection;
            
            // 更新AI决策信息
            if (debugEnabled) {
                updateAIDecision(fallbackMove, `备用逻辑 (错误: ${error.message})`);
            }
            
            processAIMove(fallbackMove);
        }
    }
}

function getCurrentDirection() {
    if (velocityX === 1) return 'right';
    if (velocityX === -1) return 'left';
    if (velocityY === 1) return 'down';
    if (velocityY === -1) return 'up';
    return 'right'; // 默认方向
}

function processAIMove(move) {
    switch(move) {
        case 'up':
            if (velocityY !== 1) {
                pendingVelocityX = 0;
                pendingVelocityY = -1;
            }
            break;
        case 'down':
            if (velocityY !== -1) {
                pendingVelocityX = 0;
                pendingVelocityY = 1;
            }
            break;
        case 'left':
            if (velocityX !== 1) {
                pendingVelocityX = -1;
                pendingVelocityY = 0;
            }
            break;
        case 'right':
            if (velocityX !== -1) {
                pendingVelocityX = 1;
                pendingVelocityY = 0;
            }
            break;
    }
}

// 调试相关函数
function toggleDebug() {
    debugEnabled = debugEnabledCheckbox.checked;
    
    if (debugEnabled) {
        debugPanel.style.display = 'block';
        if (lastGameState) {
            updateDebugInfo(lastGameState);
        }
    } else {
        debugPanel.style.display = 'none';
    }
}

function togglePause() {
    if (!gameRunning) return;
    
    gamePaused = !gamePaused;
    
    if (gamePaused) {
        startButton.textContent = '已暂停 - 按回车继续';
        startButton.disabled = true;
    } else {
        startButton.textContent = '游戏中...';
        startButton.disabled = true;
        // 重置时间以避免大的时间跳跃
        lastUpdateTime = 0;
    }
}

function updateDebugInfo(gameState) {
    const debugInfo = {
        snake_length: gameState.snake.length,
        snake_head: `(${gameState.snake[0].x}, ${gameState.snake[0].y})`,
        current_direction: gameState.currentDirection,
        foods: gameState.foods.map(f => `(${f.x}, ${f.y})`).join(', '),
        grid_size: gameState.gridSize,
        score: score,
        snake_body: gameState.snake.map(s => `(${s.x}, ${s.y})`).join(', '),
        ai_stats: {
            total_calls: aiCallCount,
            success_calls: aiSuccessCount,
            failed_calls: aiFailureCount,
            success_rate: aiCallCount > 0 ? `${((aiSuccessCount / aiCallCount) * 100).toFixed(1)}%` : '0%'
        },
        snake: gameState.snake,
        foods_array: gameState.foods
    };
    
    gameStateText.textContent = JSON.stringify(debugInfo, null, 2);
}

function updateAIDecision(move, type = 'AI决策') {
    const timestamp = new Date().toLocaleTimeString();
    const botName = currentBot ? currentBot.name : '未知机器人';
    
    aiDecisionText.innerHTML = `
        <div><strong>${type}:</strong> ${move}</div>
        <div><strong>机器人:</strong> ${botName}</div>
        <div><strong>时间:</strong> ${timestamp}</div>
    `;
} 