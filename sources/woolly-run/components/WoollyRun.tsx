
import React, { useRef, useEffect, useState, useCallback } from 'react';
import { GameState, GameMode, Player, Entity, Particle, Camera } from '../types';
import { COLORS, GRAVITY, JUMP_FORCE, GAME_WIDTH, GAME_HEIGHT, PLATFORM_CONFIG, SKINS, FRICTION, ACCELERATION, DASH_SPEED, DASH_DURATION, DASH_COOLDOWN, MAX_HEALTH, INVULNERABILITY_FRAMES, BOSS_SCORE_THRESHOLD, BOSS_HP_BASE, BOSS_HP_SCALING, ENEMY_HP, PLAYER_SHOOT_COOLDOWN, CONTROLS, HEAVY_FALL_SPEED, COOP_BOSS_HP_MULTIPLIER, COOP_BOSS_PROJ_MULTIPLIER, COOP_ENEMY_HP, GAMEPAD_MAP, GAMEPAD_DEADZONE } from '../constants';
import { RefreshCcw, Play, Trophy, Home, User, Skull, Zap, Heart, Settings, Keyboard, X, Gamepad2, CheckCircle2 } from 'lucide-react';

export const WoollyRun: React.FC = () => {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const audioCtxRef = useRef<AudioContext | null>(null);
  
  // React State for UI
  const [gameState, setGameState] = useState<GameState>(GameState.MENU);
  const [gameMode, setGameMode] = useState<GameMode>('SINGLE');
  const [score, setScore] = useState(0);
  const [highScore, setHighScore] = useState(0);
  const [showSettings, setShowSettings] = useState(false);
  
  // Gamepad Config State
  const gamepadConfigStepRef = useRef<'none' | 'p1_wait' | 'p2_wait' | 'done'>('none');
  const [gamepadConfigStep, setGamepadConfigStepState] = useState<'none' | 'p1_wait' | 'p2_wait' | 'done'>('none');
  const [gamepadsDetected, setGamepadsDetected] = useState(0);

  // Unified setter to keep Ref and State in sync
  const setGamepadConfigStep = (val: 'none' | 'p1_wait' | 'p2_wait' | 'done') => {
      gamepadConfigStepRef.current = val;
      setGamepadConfigStepState(val);
  };
  
  // Skin Selection
  const [p1Skin, setP1Skin] = useState(SKINS[0]);
  const [p2Skin, setP2Skin] = useState(SKINS[1]);

  // Boss UI
  const [bossActive, setBossActive] = useState(false);
  const [bossHp, setBossHp] = useState(0);
  const [bossMaxHp, setBossMaxHp] = useState(0);

  // HUD States for React rendering (synced from ref)
  const [playersState, setPlayersState] = useState<{hp: number, maxHp: number, dashReady: boolean, dead: boolean}[]>([]);
  
  // Refs for game loop
  const stateRef = useRef({
    players: [] as Player[],
    platforms: [] as Entity[],
    coins: [] as Entity[],
    particles: [] as Particle[],
    enemies: [] as Entity[],
    projectiles: [] as Entity[],
    clouds: [] as Entity[],
    camera: { x: 0, y: 0 } as Camera,
    pressedKeys: new Set<string>(), // Raw key codes
    gamepadAssignments: { p1: null as number | null, p2: null as number | null },
    lastJumpPressed: [false, false], // To handle "on press" for gamepads/keyboard
    lastRestartPressed: false,
    lastMenuPressed: false,
    skinSelectTimer: 0, // Throttle for stick selection
    gameSpeed: 3,
    distanceTraveled: 0,
    score: 0,
    frames: 0,
    shakeTimer: 0,
    damageFlashTimer: 0,
    lastGamepadCount: 0,
    // Boss State
    nextBossThreshold: BOSS_SCORE_THRESHOLD,
    bossMode: false,
    bossEntity: null as Entity | null,
    bossLevel: 0
  });

  const requestRef = useRef<number>(0);

  // Initialize Audio
  useEffect(() => {
    if (!audioCtxRef.current) {
        const AudioContext = window.AudioContext || (window as any).webkitAudioContext;
        if (AudioContext) audioCtxRef.current = new AudioContext();
    }
  }, []);

  const playSound = (type: 'pop' | 'dash' | 'hit' | 'die' | 'shoot' | 'bossHit' | 'bossDie' | 'charge' | 'jump' | 'heavy') => {
      if (!audioCtxRef.current) return;
      if (audioCtxRef.current.state === 'suspended') audioCtxRef.current.resume();

      const oscillator = audioCtxRef.current.createOscillator();
      const gainNode = audioCtxRef.current.createGain();
      const currTime = audioCtxRef.current.currentTime;

      switch(type) {
          case 'pop':
              oscillator.type = 'sine';
              oscillator.frequency.setValueAtTime(800, currTime);
              oscillator.frequency.exponentialRampToValueAtTime(1200, currTime + 0.1);
              gainNode.gain.setValueAtTime(0.4, currTime);
              gainNode.gain.exponentialRampToValueAtTime(0.01, currTime + 0.15);
              break;
          case 'dash':
              oscillator.type = 'triangle';
              oscillator.frequency.setValueAtTime(300, currTime);
              oscillator.frequency.linearRampToValueAtTime(100, currTime + 0.1);
              gainNode.gain.setValueAtTime(0.3, currTime);
              gainNode.gain.exponentialRampToValueAtTime(0.01, currTime + 0.1);
              break;
          case 'hit':
              oscillator.type = 'sawtooth';
              oscillator.frequency.setValueAtTime(150, currTime);
              oscillator.frequency.exponentialRampToValueAtTime(50, currTime + 0.2);
              gainNode.gain.setValueAtTime(0.4, currTime);
              gainNode.gain.linearRampToValueAtTime(0, currTime + 0.2);
              break;
          case 'die':
              // Gentle death sound
              oscillator.type = 'sine';
              oscillator.frequency.setValueAtTime(220, currTime);
              oscillator.frequency.exponentialRampToValueAtTime(55, currTime + 1.5);
              gainNode.gain.setValueAtTime(0.5, currTime);
              gainNode.gain.linearRampToValueAtTime(0, currTime + 1.5);
              break;
          case 'shoot':
              // Cute chirp
              oscillator.type = 'sine';
              oscillator.frequency.setValueAtTime(800, currTime);
              oscillator.frequency.linearRampToValueAtTime(1200, currTime + 0.08);
              gainNode.gain.setValueAtTime(0.15, currTime); 
              gainNode.gain.linearRampToValueAtTime(0, currTime + 0.08);
              break;
          case 'bossHit':
              oscillator.type = 'sawtooth';
              oscillator.frequency.setValueAtTime(100, currTime);
              gainNode.gain.setValueAtTime(0.3, currTime);
              gainNode.gain.exponentialRampToValueAtTime(0.01, currTime + 0.1);
              break;
          case 'bossDie':
              oscillator.type = 'triangle';
              oscillator.frequency.setValueAtTime(200, currTime);
              oscillator.frequency.linearRampToValueAtTime(800, currTime + 1.0);
              gainNode.gain.setValueAtTime(0.6, currTime);
              gainNode.gain.linearRampToValueAtTime(0, currTime + 1.0);
              break;
          case 'charge':
              oscillator.type = 'sine';
              oscillator.frequency.setValueAtTime(200, currTime);
              oscillator.frequency.linearRampToValueAtTime(400, currTime + 0.1);
              gainNode.gain.setValueAtTime(0.2, currTime);
              gainNode.gain.linearRampToValueAtTime(0, currTime + 0.1);
              break;
          case 'jump':
              oscillator.type = 'sine';
              oscillator.frequency.setValueAtTime(200, currTime);
              oscillator.frequency.linearRampToValueAtTime(400, currTime + 0.15);
              gainNode.gain.setValueAtTime(0.2, currTime);
              gainNode.gain.linearRampToValueAtTime(0, currTime + 0.15);
              break;
          case 'heavy':
              oscillator.type = 'sine';
              oscillator.frequency.setValueAtTime(120, currTime);
              oscillator.frequency.linearRampToValueAtTime(60, currTime + 0.3);
              gainNode.gain.setValueAtTime(0.4, currTime);
              gainNode.gain.linearRampToValueAtTime(0, currTime + 0.3);
              break;
      }
      
      oscillator.connect(gainNode);
      gainNode.connect(audioCtxRef.current.destination);
      oscillator.start();
      oscillator.stop(currTime + (type === 'bossDie' || type === 'die' ? 1.5 : 0.3));
  };

  const createPlayer = (index: number, skin: typeof SKINS[0]): Player => ({
      id: `player-${index}`,
      playerIndex: index,
      x: 100 + (index * 50),
      y: 400,
      width: 40,
      height: 40,
      color: skin.color,
      type: 'player',
      vx: 0,
      vy: 0,
      isGrounded: false,
      canDoubleJump: true,
      squashX: 1,
      squashY: 1,
      skinId: skin.id,
      skinShadow: skin.shadow,
      isDashing: false,
      isHeavyFalling: false,
      dashTimer: 0,
      dashCooldown: 0,
      facingRight: true,
      health: MAX_HEALTH,
      maxHealth: MAX_HEALTH,
      invulnerable: false,
      invulnerabilityTimer: 0,
      shootCooldown: 0,
      chargeTimer: 0,
      isDead: false
  });

  const initGame = useCallback(() => {
    // Init Players based on Mode
    stateRef.current.players = [createPlayer(0, p1Skin)];
    if (gameMode === 'COOP') {
        stateRef.current.players.push(createPlayer(1, p2Skin));
    }
    
    // Initial floor
    stateRef.current.platforms = [];
    for(let i=0; i<5; i++) {
        stateRef.current.platforms.push({
            id: `plat-init-${i}`,
            x: i * 300,
            y: GAME_HEIGHT - 100,
            width: 300,
            height: PLATFORM_CONFIG.height,
            color: COLORS.mint,
            type: 'platform',
            subtype: 'static',
            vx: 0
        });
    }

    stateRef.current.coins = [];
    stateRef.current.particles = [];
    stateRef.current.enemies = [];
    stateRef.current.projectiles = [];
    stateRef.current.clouds = [];
    
    for(let i=0; i<10; i++) {
       stateRef.current.clouds.push({
           id: `cloud-${i}`,
           x: Math.random() * GAME_WIDTH,
           y: Math.random() * (GAME_HEIGHT / 2),
           width: 60 + Math.random() * 60,
           height: 40 + Math.random() * 30,
           color: COLORS.clouds,
           type: 'cloud'
       }); 
    }

    stateRef.current.camera = { x: 0, y: 0 };
    stateRef.current.score = 0;
    stateRef.current.distanceTraveled = 0;
    stateRef.current.gameSpeed = 3;
    stateRef.current.shakeTimer = 0;
    stateRef.current.damageFlashTimer = 0;
    stateRef.current.nextBossThreshold = BOSS_SCORE_THRESHOLD;
    stateRef.current.bossMode = false;
    stateRef.current.bossEntity = null;
    stateRef.current.bossLevel = 0;
    
    setScore(0);
    setBossActive(false);
  }, [gameMode, p1Skin, p2Skin]);

  // --- Gamepad Helpers ---
  const applyDeadzone = (value: number) => {
      const absValue = Math.abs(value);
      if (absValue < GAMEPAD_DEADZONE) return 0;
      // Re-normalize to 0..1 range after deadzone
      const normalized = (absValue - GAMEPAD_DEADZONE) / (1 - GAMEPAD_DEADZONE);
      return Math.sign(value) * normalized;
  };

  const pollGamepads = () => {
      const gamepads = navigator.getGamepads();
      if (!gamepads) return;

      // Update detection count for UI
      const activeCount = Array.from(gamepads).filter(gp => gp !== null).length;
      if (activeCount !== stateRef.current.lastGamepadCount) {
          stateRef.current.lastGamepadCount = activeCount;
          setGamepadsDetected(activeCount);
      }

      // 1. Global Inputs (Restart, Menu, Skin Select)
      // Use P1's gamepad or the first active one for global menu control
      const menuGpIndex = stateRef.current.gamepadAssignments.p1 ?? Array.from(gamepads).findIndex(gp => gp !== null);
      if (menuGpIndex !== -1 && gamepads[menuGpIndex]) {
          const gp = gamepads[menuGpIndex]!;
          
          // Menu Toggle (+)
          const menuPressed = gp.buttons[GAMEPAD_MAP.MENU].pressed;
          if (menuPressed && !stateRef.current.lastMenuPressed) {
              setShowSettings(prev => !prev);
          }
          stateRef.current.lastMenuPressed = menuPressed;

          // Restart (X) - Only in Game Over
          const restartPressed = gp.buttons[GAMEPAD_MAP.RESTART].pressed;
          if (restartPressed && !stateRef.current.lastRestartPressed) {
              if (gameState === GameState.GAME_OVER) {
                  initGame();
                  setGameState(GameState.PLAYING);
              }
          }
          stateRef.current.lastRestartPressed = restartPressed;

          // Skin Select (Right Stick) - Only in Menu
          if (gameState === GameState.MENU) {
              const stickX = applyDeadzone(gp.axes[GAMEPAD_MAP.SKIN_SELECT_X]);
              if (stateRef.current.skinSelectTimer > 0) stateRef.current.skinSelectTimer--;
              
              if (Math.abs(stickX) > 0.5 && stateRef.current.skinSelectTimer === 0) {
                  const direction = Math.sign(stickX);
                  const currentIndex = SKINS.findIndex(s => s.id === p1Skin.id);
                  let nextIndex = currentIndex + direction;
                  if (nextIndex >= SKINS.length) nextIndex = 0;
                  if (nextIndex < 0) nextIndex = SKINS.length - 1;
                  setP1Skin(SKINS[nextIndex]);
                  stateRef.current.skinSelectTimer = 15; // Delay frames
              }
          }
      }

      // 2. Configuration Step
      // Use Ref to avoid stale closure issues in the game loop
      const step = gamepadConfigStepRef.current;
      
      if (step !== 'none' && step !== 'done') {
          for (const gp of gamepads) {
              if (gp && gp.buttons[GAMEPAD_MAP.CONNECT_L].pressed && gp.buttons[GAMEPAD_MAP.CONNECT_R].pressed) {
                  // Found a controller holding ZL + ZR
                  if (step === 'p1_wait') {
                      stateRef.current.gamepadAssignments.p1 = gp.index;
                      const nextStep = gameMode === 'COOP' ? 'p2_wait' : 'done';
                      setGamepadConfigStep(nextStep);
                      break;
                  } else if (step === 'p2_wait') {
                      // Ensure we don't assign P1's controller to P2
                      if (stateRef.current.gamepadAssignments.p1 !== gp.index) {
                          stateRef.current.gamepadAssignments.p2 = gp.index;
                          setGamepadConfigStep('done');
                          break;
                      }
                  }
              }
          }
      }
  };

  const getGamepadInput = (playerIdx: number) => {
      const gpIndex = playerIdx === 0 ? stateRef.current.gamepadAssignments.p1 : stateRef.current.gamepadAssignments.p2;
      if (gpIndex === null) return null;
      
      const gamepads = navigator.getGamepads();
      const gp = gamepads[gpIndex];
      if (!gp || gp.mapping !== 'standard') return null;

      return {
          moveX: applyDeadzone(gp.axes[GAMEPAD_MAP.MOVE_X]),
          jump: gp.buttons[GAMEPAD_MAP.JUMP].pressed,
          heavy: gp.buttons[GAMEPAD_MAP.HEAVY].pressed,
          dash: gp.buttons[GAMEPAD_MAP.DASH].pressed,
          attack: gp.buttons[GAMEPAD_MAP.ATTACK].pressed
      };
  };

  // --- Rendering Helpers ---

  const drawFeltRect = (
    ctx: CanvasRenderingContext2D, 
    x: number, 
    y: number, 
    w: number, 
    h: number, 
    color: string, 
    shadowColor: string = 'rgba(0,0,0,0.15)',
    radius: number = 10,
    isEnemy: boolean = false
  ) => {
    ctx.beginPath();
    ctx.roundRect(x, y, w, h, radius);
    ctx.fillStyle = color;
    ctx.shadowColor = shadowColor;
    ctx.shadowBlur = 15; 
    ctx.shadowOffsetX = 0;
    ctx.shadowOffsetY = 4;
    ctx.fill();
    ctx.shadowBlur = 0; 
    ctx.shadowOffsetY = 0;

    if (!isEnemy && w > 60) {
        ctx.strokeStyle = 'rgba(255,255,255,0.4)';
        ctx.lineWidth = 2;
        ctx.setLineDash([5, 5]);
        ctx.beginPath();
        ctx.roundRect(x + 5, y + 5, w - 10, h - 10, radius);
        ctx.stroke();
        ctx.setLineDash([]);
    } else if (isEnemy) {
        ctx.strokeStyle = 'rgba(0,0,0,0.1)';
        ctx.lineWidth = 1;
        ctx.beginPath();
        ctx.roundRect(x + 2, y + 2, w - 4, h - 4, radius);
        ctx.stroke();
    }
  };

  const drawFeltCircle = (
    ctx: CanvasRenderingContext2D,
    x: number,
    y: number,
    radius: number,
    color: string,
    stroke: boolean = false
  ) => {
    ctx.beginPath();
    ctx.arc(x, y, radius, 0, Math.PI * 2);
    ctx.fillStyle = color;
    ctx.shadowColor = 'rgba(0,0,0,0.2)';
    ctx.shadowBlur = 10;
    ctx.fill();
    ctx.shadowBlur = 0;
    if (stroke) {
        ctx.strokeStyle = 'rgba(0,0,0,0.1)';
        ctx.lineWidth = 1;
        ctx.stroke();
    }
  };

  const drawPlayer = (ctx: CanvasRenderingContext2D, player: Player, camX: number) => {
    if (player.isDead) return;

    const drawX = player.x - camX + (player.width * (1 - player.squashX)) / 2;
    const drawY = player.y + (player.height * (1 - player.squashY));
    const drawW = player.width * player.squashX;
    const drawH = player.height * player.squashY;

    if (player.invulnerable && Math.floor(Date.now() / 50) % 2 === 0) {
        ctx.globalAlpha = 0.5;
    }

    if (player.isDashing) {
        ctx.globalAlpha = 0.4;
        drawFeltRect(ctx, drawX - 20, drawY, drawW, drawH, player.color, undefined, 12);
        if (!player.invulnerable) ctx.globalAlpha = 1.0;
        
        // Aura when dashing (invincible)
        ctx.strokeStyle = '#FFFFFF';
        ctx.lineWidth = 3;
        ctx.beginPath();
        ctx.roundRect(drawX - 5, drawY - 5, drawW + 10, drawH + 10, 15);
        ctx.stroke();
    }

    // Heavy Fall Trail
    if (player.isHeavyFalling) {
        ctx.fillStyle = 'rgba(255, 255, 255, 0.4)';
        ctx.beginPath();
        ctx.rect(drawX, drawY - 40, drawW, 40);
        ctx.fill();
    }

    // Charge Aura (Mint Skin)
    if (player.skinId === 'mint' && player.chargeTimer > 0) {
        const pulse = 5 + Math.sin(Date.now() / 50) * 3;
        const chargeLevel = Math.min(5, Math.floor(player.chargeTimer / 45) + 1);
        const opacity = Math.min(0.8, chargeLevel * 0.15);
        ctx.fillStyle = `rgba(181, 234, 215, ${opacity})`;
        ctx.beginPath();
        ctx.arc(drawX + drawW/2, drawY + drawH/2, drawW/2 + pulse + (chargeLevel * 2), 0, Math.PI*2);
        ctx.fill();

        if (chargeLevel >= 5) {
             ctx.save();
             ctx.translate(drawX + drawW/2, drawY + drawH/2);
             ctx.rotate(Date.now() / 200);
             ctx.strokeStyle = 'rgba(255, 100, 100, 0.5)';
             ctx.setLineDash([10, 10]);
             ctx.lineWidth = 2;
             ctx.beginPath();
             ctx.arc(0, 0, drawW/2 + 20, 0, Math.PI*2);
             ctx.stroke();
             ctx.restore();
        }
    }

    drawFeltRect(ctx, drawX, drawY, drawW, drawH, player.color, player.skinShadow, 12);

    const lookDir = player.facingRight ? 1 : -1;
    const eyeOffsetX = lookDir * 4;
    
    // Eyes
    ctx.fillStyle = '#4A4A4A';
    ctx.beginPath();
    ctx.arc(drawX + drawW * 0.5 + eyeOffsetX - 6, drawY + drawH * 0.4, 3, 0, Math.PI * 2);
    ctx.fill();
    ctx.beginPath();
    ctx.arc(drawX + drawW * 0.5 + eyeOffsetX + 6, drawY + drawH * 0.4, 3, 0, Math.PI * 2);
    ctx.fill();

    // Blush
    ctx.fillStyle = 'rgba(255, 100, 100, 0.3)';
    ctx.beginPath();
    ctx.arc(drawX + drawW * 0.5 + eyeOffsetX - 10, drawY + drawH * 0.6, 4, 0, Math.PI * 2);
    ctx.arc(drawX + drawW * 0.5 + eyeOffsetX + 10, drawY + drawH * 0.6, 4, 0, Math.PI * 2);
    ctx.fill();
    
    // Player ID indicator for Coop
    if (gameMode === 'COOP') {
        ctx.fillStyle = '#FFF';
        ctx.font = 'bold 10px Varela Round';
        ctx.fillText(`P${player.playerIndex + 1}`, drawX + drawW/2 - 6, drawY - 8);
    }

    ctx.globalAlpha = 1.0;
  };

  const drawEntity = (ctx: CanvasRenderingContext2D, entity: Entity, camX: number) => {
      const drawX = entity.x - camX;
      
      // Flash white if damaged
      if (entity.flashTimer && entity.flashTimer > 0) {
          ctx.save();
          ctx.globalCompositeOperation = 'source-over';
      }

      if (entity.type === 'boss') {
          // Draw Boss
          const bossColor = entity.flashTimer && entity.flashTimer > 0 ? '#FFFFFF' : (entity.attackState === 'attacking' ? COLORS.bossAngry : COLORS.boss);
          drawFeltRect(ctx, drawX, entity.y, entity.width, entity.height, bossColor, '#705b82', 30, true);
          
          ctx.fillStyle = '#FFF';
          ctx.beginPath();
          ctx.arc(drawX + 30, entity.y + 40, 15, 0, Math.PI*2);
          ctx.arc(drawX + entity.width - 30, entity.y + 40, 15, 0, Math.PI*2);
          ctx.fill();
          
          ctx.fillStyle = '#000';
          ctx.beginPath();
          ctx.arc(drawX + 30, entity.y + 40, 5, 0, Math.PI*2);
          ctx.arc(drawX + entity.width - 30, entity.y + 40, 5, 0, Math.PI*2);
          ctx.fill();

          ctx.lineWidth = 5;
          ctx.strokeStyle = '#4A235A';
          ctx.beginPath();
          if (entity.attackState === 'attacking') {
             ctx.arc(drawX + entity.width/2, entity.y + 80, 20, 0, Math.PI*2);
          } else {
             ctx.moveTo(drawX + 40, entity.y + 80);
             ctx.lineTo(drawX + entity.width - 40, entity.y + 80);
          }
          ctx.stroke();

          ctx.fillStyle = 'rgba(255,255,255,0.5)';
          ctx.font = '20px Varela Round';
          ctx.fillText(`#${(entity.bossPattern || 0) + 1}`, drawX + entity.width/2 - 10, entity.y - 10);

      } else if (entity.subtype) {
          const color = entity.flashTimer && entity.flashTimer > 0 ? '#FFFFFF' : entity.color;
          if (entity.subtype === 'fly') {
              const flap = Math.sin(Date.now() / 80) * 10;
              ctx.fillStyle = color;
              ctx.beginPath();
              ctx.ellipse(drawX + 5, entity.y + 15 - flap/2, 20, 10, Math.PI / 4, 0, Math.PI * 2);
              ctx.ellipse(drawX + 35, entity.y + 15 - flap/2, 20, 10, -Math.PI / 4, 0, Math.PI * 2);
              ctx.fill();
              drawFeltCircle(ctx, drawX + 20, entity.y + 20, 15, color);
          } else if (entity.subtype === 'shooter') {
              ctx.fillStyle = color;
              ctx.fillRect(drawX, entity.y + 10, entity.width - 5, 20); 
              ctx.beginPath();
              ctx.arc(drawX + entity.width/2, entity.y + entity.height/2, 20, 0, Math.PI*2);
              ctx.fill();
          } else if (entity.subtype === 'aura') {
              const time = Date.now() / 200;
              const pulsingRadius = 25 + Math.sin(time) * 2;
              ctx.fillStyle = 'rgba(255, 111, 97, 0.2)';
              ctx.beginPath();
              ctx.arc(drawX + entity.width/2, entity.y + entity.height/2, entity.width * 0.8, 0, Math.PI*2);
              ctx.fill();
              ctx.fillStyle = color;
              ctx.beginPath();
              const spikes = 8;
              for(let i=0; i<spikes * 2; i++) {
                  const r = (i % 2 === 0) ? pulsingRadius : pulsingRadius - 10;
                  const angle = (Math.PI * i) / spikes + time;
                  const x = drawX + entity.width/2 + Math.cos(angle) * r;
                  const y = entity.y + entity.height/2 + Math.sin(angle) * r;
                  if (i===0) ctx.moveTo(x, y);
                  else ctx.lineTo(x, y);
              }
              ctx.fill();
          } else {
             drawFeltRect(ctx, drawX, entity.y, entity.width, entity.height, color, undefined, 8, true);
          }
          
          if (entity.subtype !== 'shooter' && entity.subtype !== 'aura') {
              ctx.fillStyle = '#FFF';
              ctx.beginPath();
              ctx.arc(drawX + 10, entity.y + 15, 6, 0, Math.PI*2);
              ctx.arc(drawX + 30, entity.y + 15, 6, 0, Math.PI*2);
              ctx.fill();
              ctx.fillStyle = '#000';
              ctx.beginPath();
              ctx.arc(drawX + 10, entity.y + 15, 2, 0, Math.PI*2);
              ctx.arc(drawX + 30, entity.y + 15, 2, 0, Math.PI*2);
              ctx.fill();
          }

          if (entity.hp !== undefined && entity.maxHp !== undefined && entity.maxHp > 0) {
              const barWidth = entity.width + 10;
              const barHeight = 6;
              const barX = drawX - 5;
              const barY = entity.y - 12;
              ctx.fillStyle = 'rgba(0,0,0,0.2)';
              ctx.beginPath();
              ctx.roundRect(barX, barY, barWidth, barHeight, 3);
              ctx.fill();
              const hpPercent = entity.hp / entity.maxHp;
              ctx.fillStyle = hpPercent > 0.3 ? '#66bb6a' : '#ef5350';
              ctx.beginPath();
              ctx.roundRect(barX, barY, barWidth * hpPercent, barHeight, 3);
              ctx.fill();
          }
      }

      if (entity.flashTimer && entity.flashTimer > 0) {
          ctx.restore();
          entity.flashTimer--;
      }
  };

  const spawnParticles = (x: number, y: number, color: string, count: number = 5, type: 'dust' | 'sparkle' | 'explosion' = 'dust') => {
    for (let i = 0; i < count; i++) {
        stateRef.current.particles.push({
            id: Math.random().toString(),
            x: x,
            y: y,
            vx: (Math.random() - 0.5) * (type === 'explosion' ? 20 : 10),
            vy: (Math.random() - 0.5) * (type === 'explosion' ? 20 : 10),
            life: 1.0,
            color: color,
            size: Math.random() * 6 + 2,
            type: type
        });
    }
  };

  const takeDamage = (player: Player) => {
      // DASH INVULNERABILITY: No damage if dashing
      if (player.invulnerable || player.isDashing || player.isDead) return;

      player.health -= 1;
      player.invulnerable = true;
      player.invulnerabilityTimer = INVULNERABILITY_FRAMES;
      
      stateRef.current.shakeTimer = 15;
      stateRef.current.damageFlashTimer = 5;
      
      if (player.health <= 0) {
          player.isDead = true;
          playSound('die');
          // Check global game over
          if (stateRef.current.players.every(p => p.isDead)) {
              setGameState(GameState.GAME_OVER);
          }
      } else {
          playSound('hit');
      }
  };

  const handleJump = (player: Player) => {
    if (player.isDead) return;
    
    if (player.isGrounded) {
        player.vy = JUMP_FORCE;
        player.squashX = 0.7; player.squashY = 1.3;
        spawnParticles(player.x + player.width/2, player.y + player.height, '#FFF', 3, 'dust');
        playSound('jump');
        player.isGrounded = false;
    } else if (player.canDoubleJump) {
        player.vy = JUMP_FORCE * 0.9;
        player.canDoubleJump = false;
        player.squashX = 0.6; player.squashY = 1.4;
        spawnParticles(player.x + player.width/2, player.y + player.height, '#FFF', 5, 'dust');
        playSound('jump');
        player.isHeavyFalling = false;
    }
  };

  // --- Boss Logic Helpers ---
  const spawnBoss = (x: number, y: number) => {
      const level = stateRef.current.bossLevel;
      const isCoop = gameMode === 'COOP';
      
      let hp = BOSS_HP_BASE;
      if (isCoop) hp = Math.ceil(hp * COOP_BOSS_HP_MULTIPLIER);
      
      stateRef.current.bossEntity = {
          id: 'boss',
          x: x,
          y: y,
          width: 100,
          height: 100,
          color: COLORS.boss,
          type: 'boss',
          hp: hp,
          maxHp: hp,
          vx: 0,
          vy: 0,
          bossPattern: level % 10,
          attackState: 'idle',
          attackTimer: 60,
          targetY: y
      };
      setBossActive(true);
      setBossHp(hp);
      setBossMaxHp(hp);
  };

  const updateBoss = (boss: Entity) => {
      boss.y = (boss.targetY || 400) + Math.sin(Date.now() / 300) * 20;

      if (boss.flashTimer && boss.flashTimer > 0) boss.flashTimer--;
      if (!boss.attackTimer) boss.attackTimer = 0;
      boss.attackTimer--;
      
      const isCoop = gameMode === 'COOP';
      const projMult = isCoop ? COOP_BOSS_PROJ_MULTIPLIER : 1;
      
      // Target random alive player
      const alivePlayers = stateRef.current.players.filter(p => !p.isDead);
      const targetPlayer = alivePlayers.length > 0 ? alivePlayers[Math.floor(Math.random() * alivePlayers.length)] : stateRef.current.players[0];

      if (boss.attackTimer <= 0 && targetPlayer) {
          boss.attackState = 'attacking';
          boss.color = COLORS.bossAngry;

          const pattern = boss.bossPattern || 0;
          const centerX = boss.x + boss.width/2;
          const centerY = boss.y + boss.height/2;

          switch(pattern) {
              case 0: // Ripple
                  for(let i=0; i<Math.ceil(3 * projMult); i++) {
                       setTimeout(() => {
                           shootProjectile(centerX, boss.y + boss.height - 20, -8 - i, 0, 'enemy', 30);
                       }, i * 300);
                  }
                  boss.attackTimer = 180;
                  break;
              case 1: // Scatter
                   const count = Math.ceil(3 * projMult);
                   for(let i=0; i<count; i++) {
                       const vy = (i - Math.floor(count/2)) * 2;
                       shootProjectile(centerX, centerY, -8, vy, 'enemy');
                   }
                   boss.attackTimer = 120;
                   break;
              case 2: // Sniper
                   shootProjectile(centerX, centerY, -15, (targetPlayer.y - centerY) * 0.05, 'enemy', 20);
                   boss.attackTimer = 150;
                   break;
              case 3: // Rain
                   for(let i=0; i<Math.ceil(5 * projMult); i++) {
                       setTimeout(() => {
                           const t = alivePlayers[Math.floor(Math.random() * alivePlayers.length)];
                           const targetX = t.x + (Math.random() - 0.5) * 400;
                           shootProjectile(targetX, 50, 0, 5, 'enemy', 30);
                       }, i * 200);
                   }
                   boss.attackTimer = 200;
                   break;
              case 4: // Charger
                   boss.vx = -15;
                   setTimeout(() => { boss.vx = 15; }, 1000); 
                   setTimeout(() => { boss.vx = 0; }, 2000);
                   boss.attackTimer = 240;
                   break;
              case 5: // Bouncer
                   for(let i=0; i<Math.ceil(2 * projMult); i++) {
                       shootProjectile(centerX, centerY, -6 - i, 8, 'enemy', 25);
                   }
                   boss.attackTimer = 140;
                   break;
              case 6: // Wall
                   for(let i=0; i<Math.ceil(5 * projMult); i++) {
                       shootProjectile(centerX, boss.y + i * 25, -6, 0, 'enemy');
                   }
                   boss.attackTimer = 180;
                   break;
              case 7: // Spiral
                   const spokes = Math.ceil(8 * projMult);
                   for(let i=0; i<spokes; i++) {
                       const angle = (Math.PI * 2 * i) / spokes;
                       shootProjectile(centerX, centerY, Math.cos(angle) * 5, Math.sin(angle) * 5, 'enemy');
                   }
                   boss.attackTimer = 160;
                   break;
              case 8: // Homing
                   shootProjectile(centerX, centerY, -4, (targetPlayer.y - centerY)*0.01, 'enemy');
                   if(isCoop) setTimeout(() => shootProjectile(centerX, centerY, -4, (targetPlayer.y - centerY)*0.01, 'enemy'), 500);
                   boss.attackTimer = 180;
                   break;
              case 9: // Chaos
                   if (Math.random() > 0.5) { 
                       for(let i=0; i<3; i++) shootProjectile(targetPlayer.x + (Math.random()-0.5)*300, 50, 0, 8, 'enemy');
                   } else { 
                       shootProjectile(centerX, centerY, -10, -5, 'enemy');
                       shootProjectile(centerX, centerY, -10, 5, 'enemy');
                   }
                   boss.attackTimer = 100;
                   break;
          }

          setTimeout(() => {
              boss.attackState = 'idle';
              boss.color = COLORS.boss;
          }, 1000);
      }

      boss.x += (boss.vx || 0);
  };

  const shootProjectile = (x: number, y: number, vx: number, vy: number, owner: 'player' | 'enemy', size: number = 15, damage: number = 1) => {
      stateRef.current.projectiles.push({
          id: `proj-${Date.now()}-${Math.random()}`,
          x: x,
          y: y,
          width: size,
          height: size,
          color: owner === 'player' ? COLORS.projectilePlayer : COLORS.projectileEnemy,
          type: 'projectile',
          subtype: 'normal',
          owner: owner,
          vx: vx,
          vy: vy,
          damage: damage
      });
      if (owner === 'player') playSound('shoot');
  };

  const spawnPlayerProjectile = (player: Player) => {
      const startX = player.x + (player.facingRight ? player.width : -20);
      const startY = player.y + player.height/2 - 10;
      const direction = player.facingRight ? 1 : -1;
      
      if (player.skinId === 'berry') {
          stateRef.current.projectiles.push({
              id: `proj-${Date.now()}`, x: startX, y: startY, width: 20, height: 20,
              color: '#FF9AA2', type: 'projectile', subtype: 'homing', owner: 'player',
              vx: 12 * direction, vy: 0, damage: 0.5
          });
      } else if (player.skinId === 'ice') {
          for(let i = -1; i <= 1; i++) {
              stateRef.current.projectiles.push({
                  id: `proj-${Date.now()}-${i}`, x: startX, y: startY, width: 15, height: 15,
                  color: '#A0E7E5', type: 'projectile', subtype: 'shotgun', owner: 'player',
                  vx: 20 * direction, vy: i * 3, damage: 1
              });
          }
      } else if (player.skinId === 'lemon') {
          stateRef.current.projectiles.push({
              id: `proj-${Date.now()}`, x: startX, y: startY, width: 20, height: 20,
              color: '#FBE7C6', type: 'projectile', subtype: 'burst', owner: 'player',
              vx: 8 * direction, vy: 0, damage: 2, startX: startX
          });
      } else if (player.skinId === 'mint') {
          // Handled via release
      } else {
          stateRef.current.projectiles.push({
               id: `proj-${Date.now()}`, x: startX, y: startY, width: 20, height: 20,
               color: player.color, type: 'projectile', subtype: 'normal', owner: 'player',
               vx: 24 * direction, vy: 0, damage: 1
          });
      }
      playSound('shoot');
  };

  const fireMintShot = (player: Player) => {
      const chargeFramesPerLevel = 45;
      const chargeLevel = Math.floor(player.chargeTimer / chargeFramesPerLevel);
      const damage = Math.min(5, 1 + chargeLevel);
      
      const startX = player.x + (player.facingRight ? player.width : -20);
      const direction = player.facingRight ? 1 : -1;
      const size = 20 + (damage * 5);
      
      stateRef.current.projectiles.push({
          id: `proj-${Date.now()}`, x: startX, y: player.y + player.height/2 - size/2, 
          width: size, height: size,
          color: '#B5EAD7', type: 'projectile', subtype: 'charged', owner: 'player',
          vx: 18 * direction, vy: 0, damage: damage
      });
      playSound('shoot');
  };

  const adjustAngle = (current: number, target: number, maxChange: number) => {
      let diff = target - current;
      while (diff <= -Math.PI) diff += Math.PI * 2;
      while (diff > Math.PI) diff -= Math.PI * 2;
      if (Math.abs(diff) < maxChange) return target;
      return current + Math.sign(diff) * maxChange;
  };

  const update = () => {
    // POLL GAMEPADS every frame
    pollGamepads();

    const state = stateRef.current;
    
    // 1. Global Effects
    if (state.shakeTimer > 0) state.shakeTimer--;
    if (state.damageFlashTimer > 0) state.damageFlashTimer--;

    // 2. Difficulty
    if (!state.bossMode) {
        state.gameSpeed = 3 + (state.distanceTraveled / 5000); 
        if (state.score >= state.nextBossThreshold && !state.bossEntity) {
             state.bossMode = true;
        }
    }

    // 3. Player Updates Loop
    state.players.forEach((player, idx) => {
        if (player.isDead) return;

        // Squash fix
        player.squashX += (1 - player.squashX) * 0.1;
        player.squashY += (1 - player.squashY) * 0.1;
        
        if (player.invulnerable) {
            player.invulnerabilityTimer--;
            if (player.invulnerabilityTimer <= 0) player.invulnerable = false;
        }
        if (player.shootCooldown > 0) player.shootCooldown--;

        // -- INPUT HANDLING (Merged Keyboard + Gamepad) --
        const pControls = player.playerIndex === 0 ? CONTROLS.P1 : CONTROLS.P2;
        const gpInput = getGamepadInput(player.playerIndex);
        
        const isKeyPressed = (keys: string[]) => keys.some(k => state.pressedKeys.has(k));

        const keyLeft = isKeyPressed(pControls.LEFT) || (gpInput && gpInput.moveX < -0.3);
        const keyRight = isKeyPressed(pControls.RIGHT) || (gpInput && gpInput.moveX > 0.3);
        
        // JUMP Logic (On Press & Variable Height)
        const rawJump = isKeyPressed(pControls.UP) || (gpInput && gpInput.jump);
        const jumpPressedNow = rawJump && !state.lastJumpPressed[idx];
        
        // Variable Jump Height (Cutting velocity on release)
        // Detect falling edge: Was pressed last frame, is not pressed now
        if (state.lastJumpPressed[idx] && !rawJump) {
             if (player.vy < -5) player.vy *= 0.5;
        }

        state.lastJumpPressed[idx] = !!rawJump;
        
        if (jumpPressedNow) {
            handleJump(player);
        }

        const keyShoot = isKeyPressed(pControls.SHOOT) || (gpInput && gpInput.attack);
        const keyDash = isKeyPressed(pControls.DASH) || (gpInput && gpInput.dash);
        const keyHeavy = isKeyPressed(pControls.HEAVY) || (gpInput && gpInput.heavy);

        // Combat Input
        if (player.skinId === 'mint') {
            if (keyShoot) {
                player.chargeTimer++;
                if (player.chargeTimer % 20 === 0) playSound('charge');
            } else {
                if (player.chargeTimer > 0) {
                    fireMintShot(player);
                    player.chargeTimer = 0;
                }
            }
        } else {
            if (keyShoot && player.shootCooldown <= 0) {
                spawnPlayerProjectile(player);
                player.shootCooldown = PLAYER_SHOOT_COOLDOWN;
            }
        }

        // Heavy Fall Input
        if (keyHeavy && !player.isGrounded && !player.isHeavyFalling && !player.isDashing) {
            player.isHeavyFalling = true;
            player.vy = HEAVY_FALL_SPEED;
            // playSound('heavy'); // Plays on impact
        }

        // Dash Input
        if (player.dashCooldown > 0) player.dashCooldown--;
        
        if (keyDash && player.dashCooldown === 0 && !player.isDashing) {
            player.isDashing = true;
            player.dashTimer = DASH_DURATION;
            player.dashCooldown = DASH_COOLDOWN;
            player.vx = player.facingRight ? DASH_SPEED : -DASH_SPEED;
            player.vy = 0;
            player.isHeavyFalling = false; // Dash cancels heavy fall
            playSound('dash');
            spawnParticles(player.x, player.y + player.height/2, '#FFF', 10, 'dust');
        }

        // Movement Physics
        if (player.isDashing) {
            player.dashTimer--;
            player.vy = 0;
            if (player.dashTimer <= 0) {
                player.isDashing = false;
                player.vx *= 0.5;
            }
        } else {
            // Check Analog X first, if not present use digital keys
            let moveInput = 0;
            if (gpInput && Math.abs(gpInput.moveX) > 0) {
                 moveInput = gpInput.moveX; // -1 to 1
                 if (moveInput > 0) player.facingRight = true;
                 else if (moveInput < 0) player.facingRight = false;
            } else {
                 if (keyLeft) { moveInput = -1; player.facingRight = false; }
                 if (keyRight) { moveInput = 1; player.facingRight = true; }
            }
            
            player.vx += moveInput * ACCELERATION;
            player.vx *= FRICTION; 
            
            // Heavy fall overrides gravity logic slightly (already set high vy)
            if (!player.isHeavyFalling) {
                player.vy += GRAVITY;
            }
        }

        player.x += player.vx;
        player.y += player.vy;

        // Boss Wall
        if (state.bossEntity) {
            const wallX = state.bossEntity.x;
            if (player.x + player.width > wallX) {
                player.x = wallX - player.width;
                if (player.vx > 0) player.vx = 0;
            }
        }

        // Death Pits
        if (player.y > GAME_HEIGHT) {
            player.health = 0;
            takeDamage(player); // Trigger death logic
        }
        if (player.x < state.camera.x - 50) {
            // Off screen death
            player.health = 0;
            takeDamage(player);
        }

        // Platform Collisions
        const wasGrounded = player.isGrounded;
        player.isGrounded = false;
        
        for (const plat of state.platforms) {
            if (plat.subtype === 'moving') {
                 if (!plat.startX) plat.startX = plat.x;
                 plat.x += (plat.vx || 0);
                 if (Math.abs(plat.x - plat.startX) > 150) plat.vx = -(plat.vx || 2);
            }

            const overlapStart = Math.max(player.x, plat.x);
            const overlapEnd = Math.min(player.x + player.width, plat.x + plat.width);
            const overlapWidth = Math.max(0, overlapEnd - overlapStart);
            const isSupported = overlapWidth > (player.width * 0.66);

            if (isSupported && player.y + player.height > plat.y && player.y + player.height < plat.y + plat.height + 35 && player.vy >= 0) {
                if (!wasGrounded) {
                     player.squashX = 1.3;
                     player.squashY = 0.7;
                     // Heavy Fall Impact
                     if (player.isHeavyFalling) {
                         spawnParticles(player.x + player.width/2, player.y + player.height, '#FFF', 20, 'dust');
                         state.shakeTimer = 10;
                         playSound('heavy');
                         player.isHeavyFalling = false;
                     }
                }
                player.isGrounded = true;
                player.vy = 0;
                player.y = plat.y - player.height;
                player.canDoubleJump = true; // Reset Double Jump
                if (plat.subtype === 'moving') player.x += (plat.vx || 0);
            }
        }
    });

    // Coop Tether Logic: Stop movement if one player is about to leave screen
    if (gameMode === 'COOP' && state.players.length === 2 && !state.players[0].isDead && !state.players[1].isDead) {
        const p1 = state.players[0];
        const p2 = state.players[1];
        const maxDist = 700; // Constrain to screen width

        if (Math.abs(p1.x - p2.x) > maxDist) {
            if (p1.x > p2.x) {
                p1.x = p2.x + maxDist;
                if (p1.vx > 0) p1.vx = 0;
            } else {
                p2.x = p1.x + maxDist;
                if (p2.vx > 0) p2.vx = 0;
            }
        }
    }

    // 4. Update Camera (Follow Leader)
    const alivePlayers = state.players.filter(p => !p.isDead);
    if (alivePlayers.length > 0) {
        const leaderX = Math.max(...alivePlayers.map(p => p.x));
        const offset = gameMode === 'COOP' ? 800 : GAME_WIDTH * 0.3; // Give more space in Co-op for trailing player
        const targetCamX = leaderX - offset;
        state.camera.x += (targetCamX - state.camera.x) * 0.1;
        state.distanceTraveled = Math.max(state.distanceTraveled, leaderX);
    }

    // 5. Boss Logic
    if (state.bossEntity) {
        updateBoss(state.bossEntity);
        // Collision
        state.players.forEach(p => {
            if (!p.isDead &&
                p.x < state.bossEntity!.x + state.bossEntity!.width &&
                p.x + p.width > state.bossEntity!.x &&
                p.y < state.bossEntity!.y + state.bossEntity!.height &&
                p.y + p.height > state.bossEntity!.y
            ) {
                takeDamage(p);
            }
        });
    }

    // 6. Entity Logic
    for (let i = state.enemies.length - 1; i >= 0; i--) {
        const enemy = state.enemies[i];
        // ... (Movement logic same as before) ...
        if (enemy.subtype === 'patrol') {
            if (!enemy.startX) enemy.startX = enemy.x;
            enemy.x += (enemy.vx || 1);
            if (Math.abs(enemy.x - enemy.startX) > (enemy.patrolRange || 100)) enemy.vx = -(enemy.vx || 1);
        } else if (enemy.subtype === 'fly') {
             enemy.x -= 4; 
             enemy.y += Math.sin(Date.now() / 200) * 1; 
        } else if (enemy.subtype === 'shooter') {
            if (!enemy.shootTimer) enemy.shootTimer = 0;
            enemy.shootTimer--;
            if (enemy.shootTimer <= 0) {
                // Aim at nearest player
                let target = state.players[0];
                let minDist = 10000;
                state.players.forEach(p => {
                    if (p.isDead) return;
                    const d = Math.hypot(p.x - enemy.x, p.y - enemy.y);
                    if (d < minDist) { minDist = d; target = p; }
                });

                const dx = (target.x + target.width/2) - (enemy.x);
                const dy = (target.y + target.height/2) - (enemy.y + 10);
                const angle = Math.atan2(dy, dx);
                shootProjectile(enemy.x, enemy.y + 10, Math.cos(angle) * 6, Math.sin(angle) * 6, 'enemy');
                enemy.shootTimer = 180;
            }
        } else if (enemy.subtype === 'aura') {
             if (enemy.startY === undefined) enemy.startY = enemy.y;
             enemy.y = enemy.startY + Math.sin(Date.now() / 300) * 80;
        }

        // Collision with Players
        let hitBoxX = enemy.x;
        let hitBoxY = enemy.y;
        let hitBoxW = enemy.width;
        let hitBoxH = enemy.height;
        if (enemy.subtype === 'aura') { hitBoxX -= 10; hitBoxY -= 10; hitBoxW += 20; hitBoxH += 20; }

        state.players.forEach(p => {
            if (!p.isDead &&
                p.x < hitBoxX + hitBoxW - 10 &&
                p.x + p.width > hitBoxX + 10 &&
                p.y < hitBoxY + hitBoxH - 10 &&
                p.y + p.height > hitBoxY + 10
            ) {
                 takeDamage(p);
            }
        });

        if (enemy.x - state.camera.x < -200) state.enemies.splice(i, 1);
        else if (enemy.hp !== undefined && enemy.hp <= 0) {
            spawnParticles(enemy.x + enemy.width/2, enemy.y + enemy.height/2, enemy.color, 10, 'explosion');
            playSound('pop');
            state.score += 50;
            state.enemies.splice(i, 1);
        }
    }

    // 7. Projectile Logic
    for (let i = state.projectiles.length - 1; i >= 0; i--) {
        const proj = state.projectiles[i];
        
        if (proj.owner === 'player') {
             // Burst/Homing Logic
             if (proj.subtype === 'homing' || (proj.subtype === 'burst' && proj.damage === 1)) {
                 // Targeting logic
                 let target = null;
                 const bossVisible = state.bossEntity && state.bossEntity.hp && state.bossEntity.hp > 0 &&
                                    (state.bossEntity.x - state.camera.x < GAME_WIDTH) && (state.bossEntity.x - state.camera.x > 0);
                 
                 if (bossVisible) {
                     target = state.bossEntity;
                 } else {
                     let minDist = 600;
                     for (const t of state.enemies) {
                         if (t.x > proj.x) {
                             const d = Math.hypot((t.x + t.width/2) - proj.x, (t.y + t.height/2) - proj.y);
                             if (d < minDist) { minDist = d; target = t; }
                         }
                     }
                 }

                 if (target) {
                     const tx = target.x + target.width/2;
                     const ty = target.y + target.height/2;
                     const angle = Math.atan2(ty - proj.y, tx - proj.x);
                     if (proj.subtype === 'homing') {
                         const currentAngle = Math.atan2(proj.vy!, proj.vx!);
                         const newAngle = adjustAngle(currentAngle, angle, 0.15);
                         proj.vx = Math.cos(newAngle) * 12;
                         proj.vy = Math.sin(newAngle) * 12;
                     } else {
                         // Burst locked
                         if (Math.abs(proj.vx!) < 10) { // Only redirect on burst trigger
                            proj.vx = Math.cos(angle) * 35;
                            proj.vy = Math.sin(angle) * 35;
                         }
                     }
                 } else if (proj.subtype === 'burst' && proj.damage === 1) {
                     // NO TARGET but BURST triggered: Go straight forward fast
                     if (Math.abs(proj.vx!) < 10) {
                         const dir = proj.vx! > 0 ? 1 : -1;
                         proj.vx = dir * 35;
                         proj.vy = 0;
                     }
                 }
             }
             
             // Burst Trigger check
             if (proj.subtype === 'burst' && proj.startX !== undefined) {
                 const dist = Math.abs(proj.x - proj.startX);
                 if (dist > 300 && Math.abs(proj.vx || 0) < 10) {
                     proj.damage = 1;
                     proj.color = '#FFA500';
                     // Note: Target acquisition happens in the block above on the NEXT frame usually, 
                     // or we can force a check now, but the loop order handles it fine next frame or immediately if structure allows.
                     // The logic above checks (subtype === burst && damage === 1). 
                     // So once we set damage = 1 here, the block above will execute next frame to aim or shoot straight.
                 }
             }
        }

        proj.x += (proj.vx || 0);
        proj.y += (proj.vy || 0);

        // Collisions
        if (proj.owner === 'player') {
            const dmg = proj.damage || 1;
            let hit = false;
            // Vs Enemies
            for(const enemy of state.enemies) {
                if (proj.x < enemy.x + enemy.width && proj.x + proj.width > enemy.x &&
                    proj.y < enemy.y + enemy.height && proj.y + proj.height > enemy.y) {
                     enemy.hp = (enemy.hp || ENEMY_HP) - dmg;
                     enemy.flashTimer = 5;
                     spawnParticles(enemy.x + enemy.width/2, enemy.y + enemy.height/2, enemy.color, 3, 'sparkle');
                     hit = true;
                     break; 
                }
            }
            // Vs Boss
            if (!hit && state.bossEntity) {
                const b = state.bossEntity;
                if (proj.x < b.x + b.width && proj.x + proj.width > b.x &&
                    proj.y < b.y + b.height && proj.y + proj.height > b.y) {
                    b.hp = (b.hp || BOSS_HP_BASE) - dmg;
                    b.flashTimer = 5;
                    setBossHp(b.hp);
                    playSound('bossHit');
                    spawnParticles(proj.x, proj.y, b.color, 5, 'sparkle');
                    hit = true;
                    if (b.hp <= 0) {
                        playSound('bossDie');
                        spawnParticles(b.x + b.width/2, b.y + b.height/2, COLORS.boss, 50, 'explosion');
                        state.bossEntity = null;
                        setBossActive(false);
                        state.bossMode = false;
                        setScore(state.score);
                        state.nextBossThreshold += BOSS_SCORE_THRESHOLD;
                        state.bossLevel++;
                        // Heal living players
                        state.players.forEach(p => {
                            if (!p.isDead) {
                                p.health = Math.min(p.health + 1, MAX_HEALTH);
                            }
                        });
                    }
                }
            }
            if (hit) state.projectiles.splice(i, 1);
        } else {
            // Vs Players
            state.players.forEach(p => {
                if (!p.isDead &&
                    p.x < proj.x + proj.width &&
                    p.x + p.width > proj.x &&
                    p.y < proj.y + proj.height &&
                    p.y + p.height > proj.y
                ) {
                     takeDamage(p);
                     state.projectiles.splice(i, 1);
                }
            });
        }
        
        if (proj.x - state.camera.x < -200 || proj.x - state.camera.x > GAME_WIDTH + 200 || proj.y > GAME_HEIGHT || proj.y < -100) {
            state.projectiles.splice(i, 1);
        }
    }

    // 8. Coins
    for (let i = state.coins.length - 1; i >= 0; i--) {
        const coin = state.coins[i];
        let collected = false;
        state.players.forEach(p => {
            if (p.isDead) return;
            const dx = (p.x + p.width/2) - (coin.x + coin.width/2);
            const dy = (p.y + p.height/2) - (coin.y + coin.height/2);
            if (Math.sqrt(dx*dx + dy*dy) < 40) collected = true;
        });

        if (collected) {
            state.score += 100;
            setScore(state.score);
            spawnParticles(coin.x + 15, coin.y + 15, COLORS.coin, 12, 'sparkle');
            playSound('pop'); 
            state.coins.splice(i, 1);
        }
    }

    // 9. Procedural Generation (Same logic, tracking right-most platform)
    const lastPlatform = state.platforms[state.platforms.length - 1];
    if (lastPlatform && (lastPlatform.x - state.camera.x) < GAME_WIDTH + 100) {
        if (state.bossMode && !state.bossEntity) {
            // Spawn Arena
            const arenaX = lastPlatform.x + lastPlatform.width + 100;
            const arenaY = GAME_HEIGHT / 2 + 100;
            state.platforms.push({
                id: `arena-${state.bossLevel}`, x: arenaX, y: arenaY, width: 1200, height: 40,
                color: COLORS.arena, type: 'platform', subtype: 'static', vx: 0
            });
            spawnBoss(arenaX + 800, arenaY - 150);
        } else if (!state.bossMode) {
            const gap = PLATFORM_CONFIG.minGap + Math.random() * (PLATFORM_CONFIG.maxGap - PLATFORM_CONFIG.minGap);
            const width = PLATFORM_CONFIG.minWidth + Math.random() * (PLATFORM_CONFIG.maxWidth - PLATFORM_CONFIG.minWidth);
            let heightChange = (Math.random() - 0.5) * 350;
            let y = lastPlatform.y + heightChange;
            if (y < 200) y = 200;
            if (y > GAME_HEIGHT - 100) y = GAME_HEIGHT - 100;

            const newPlatX = lastPlatform.x + lastPlatform.width + gap;
            const platColors = [COLORS.mint, COLORS.pink, COLORS.yellow, COLORS.lavender, COLORS.purple];
            let color = platColors[Math.floor(Math.random() * platColors.length)];
            const isMoving = Math.random() < 0.2; 
            if (isMoving) color = COLORS.movingPlat;

            state.platforms.push({
                id: `plat-${newPlatX}`, x: newPlatX, y: y, width: width, height: PLATFORM_CONFIG.height,
                color: color, type: 'platform', subtype: isMoving ? 'moving' : 'static', vx: isMoving ? 2 : 0
            });

            // Enemies (Scaled HP)
            const hasEnemy = state.score > 300 && Math.random() > 0.5;
            if (hasEnemy && !isMoving) {
                const rand = Math.random();
                let enemyType: 'patrol' | 'fly' | 'shooter' | 'aura' = 'patrol';
                if (rand < 0.3) enemyType = 'patrol';
                else if (rand < 0.6) enemyType = 'fly';
                else if (rand < 0.8) enemyType = 'shooter';
                else enemyType = 'aura';

                const hp = gameMode === 'COOP' ? COOP_ENEMY_HP : ENEMY_HP;
                const enemyCommon = { width: 40, height: 40, type: 'enemy' as const, hp: hp, maxHp: hp };

                if (enemyType === 'patrol' && width > 150) {
                     state.enemies.push({ ...enemyCommon, id: `enemy-${newPlatX}`, x: newPlatX + width / 2, y: y - 40, color: COLORS.enemyPatrol, subtype: 'patrol', vx: 1, patrolRange: width / 2 - 20 });
                } else if (enemyType === 'fly') {
                     state.enemies.push({ ...enemyCommon, id: `enemy-fly-${newPlatX}`, x: newPlatX - gap/2, y: y - 100 - Math.random() * 100, color: COLORS.enemyFly, subtype: 'fly' });
                } else if (enemyType === 'shooter') {
                     state.enemies.push({ ...enemyCommon, id: `enemy-shoot-${newPlatX}`, x: newPlatX + width - 50, y: y - 40, color: COLORS.enemyShooter, subtype: 'shooter', shootTimer: 60 });
                } else if (enemyType === 'aura') {
                     state.enemies.push({ ...enemyCommon, id: `enemy-aura-${newPlatX}`, x: newPlatX + width / 2, y: y - 50, color: COLORS.enemyAura, subtype: 'aura' });
                }
            }

            if (!hasEnemy || Math.random() > 0.5) {
                state.coins.push({ id: `coin-${newPlatX}`, x: newPlatX + width / 2 - 15, y: y - 80, width: 30, height: 30, color: COLORS.coin, type: 'coin' });
            }
        }
    }

    if (state.platforms.length > 20) state.platforms.shift();
    if (state.coins.length > 20) state.coins.shift();
    
    // Cleanup
    for (let i = state.particles.length - 1; i >= 0; i--) {
        const p = state.particles[i];
        p.x += p.vx; p.y += p.vy; p.life -= 0.05;
        if (p.type === 'sparkle') { p.vy -= 0.1; p.size = Math.random() * 5 + 2; } else p.vy += 0.2; 
        if (p.life <= 0) state.particles.splice(i, 1);
    }
    
    // Sync React UI State
    setPlayersState(state.players.map(p => ({
        hp: p.health, maxHp: p.maxHealth, dashReady: p.dashCooldown === 0, dead: p.isDead
    })));
  };

  const draw = () => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const ctx = canvas.getContext('2d');
    if (!ctx) return;
    const state = stateRef.current;

    // Shake
    ctx.save();
    if (state.shakeTimer > 0) {
        ctx.translate((Math.random() - 0.5) * 10, (Math.random() - 0.5) * 10);
    }

    // BG
    ctx.fillStyle = COLORS.sky;
    ctx.fillRect(-10, -10, GAME_WIDTH + 20, GAME_HEIGHT + 20);

    // Clouds
    state.clouds.forEach(cloud => {
        const drawX = cloud.x - state.camera.x * 0.2; 
        ctx.fillStyle = COLORS.clouds;
        ctx.shadowColor = 'rgba(255,255,255,0.8)';
        ctx.shadowBlur = 20;
        ctx.beginPath();
        ctx.arc(drawX, cloud.y, cloud.width/2, 0, Math.PI*2);
        ctx.arc(drawX - cloud.width/3, cloud.y + 10, cloud.width/3, 0, Math.PI*2);
        ctx.arc(drawX + cloud.width/3, cloud.y + 10, cloud.width/3, 0, Math.PI*2);
        ctx.fill();
        ctx.shadowBlur = 0;
    });

    state.platforms.forEach(plat => {
        if (plat.x - state.camera.x > -1200 && plat.x - state.camera.x < GAME_WIDTH + 100) {
            drawFeltRect(ctx, plat.x - state.camera.x, plat.y, plat.width, plat.height, plat.color);
        }
    });

    state.enemies.forEach(enemy => drawEntity(ctx, enemy, state.camera.x));
    if (state.bossEntity) drawEntity(ctx, state.bossEntity, state.camera.x);

    state.projectiles.forEach(proj => {
        drawFeltCircle(ctx, proj.x - state.camera.x, proj.y, proj.width/2, proj.color);
    });

    state.coins.forEach(coin => {
        const floatY = Math.sin(Date.now() / 200) * 5;
        drawFeltCircle(ctx, coin.x - state.camera.x + 15, coin.y + floatY + 15, 12, COLORS.coin);
        ctx.fillStyle = '#FFF';
        ctx.beginPath();
        ctx.arc(coin.x - state.camera.x + 10, coin.y + floatY + 10, 4, 0, Math.PI*2);
        ctx.fill();
    });

    state.particles.forEach(p => {
        ctx.globalAlpha = p.life;
        if (p.type === 'sparkle') { ctx.shadowColor = '#FFF'; ctx.shadowBlur = 10; }
        drawFeltCircle(ctx, p.x - state.camera.x, p.y, p.size, p.color);
        ctx.shadowBlur = 0;
        ctx.globalAlpha = 1.0;
    });

    state.players.forEach(p => drawPlayer(ctx, p, state.camera.x));

    if (state.damageFlashTimer > 0) {
        ctx.fillStyle = `rgba(255, 0, 0, ${state.damageFlashTimer * 0.1})`;
        ctx.fillRect(0, 0, GAME_WIDTH, GAME_HEIGHT);
    }

    ctx.restore();

    // Vignette
    const gradient = ctx.createRadialGradient(GAME_WIDTH/2, GAME_HEIGHT/2, GAME_HEIGHT/3, GAME_WIDTH/2, GAME_HEIGHT/2, GAME_HEIGHT);
    gradient.addColorStop(0, 'rgba(255,255,255,0)');
    gradient.addColorStop(1, 'rgba(255,183,178,0.1)');
    ctx.fillStyle = gradient;
    ctx.fillRect(0,0, GAME_WIDTH, GAME_HEIGHT);
  };

  const gameLoop = () => {
    if (gameState === GameState.PLAYING) {
      update();
    } else if (gameState === GameState.MENU && showSettings) {
        // Run polling logic even in menu to detect connection logic
        pollGamepads();
    }
    draw();
    requestRef.current = requestAnimationFrame(gameLoop);
  };

  // --- Inputs ---

  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      // PREVENT REPEAT: Check if key is already known to be pressed
      if (stateRef.current.pressedKeys.has(e.code)) return;
      stateRef.current.pressedKeys.add(e.code);

      if (gameState === GameState.MENU) {
          if (e.code === 'Escape') {
              setShowSettings(prev => !prev);
              return;
          }
      }
      if (gameState === GameState.GAME_OVER && (e.code === 'KeyR')) {
           initGame();
           setGameState(GameState.PLAYING);
           return;
      }
      
      // MOVED: Jump logic is now fully handled in update() loop to prevent double triggers
    };

    const handleKeyUp = (e: KeyboardEvent) => {
      stateRef.current.pressedKeys.delete(e.code);
      // MOVED: Variable jump height logic is now fully handled in update() loop
    };

    window.addEventListener('keydown', handleKeyDown);
    window.addEventListener('keyup', handleKeyUp);
    requestRef.current = requestAnimationFrame(gameLoop);
    return () => {
      window.removeEventListener('keydown', handleKeyDown);
      window.removeEventListener('keyup', handleKeyUp);
      if (requestRef.current) cancelAnimationFrame(requestRef.current);
    };
  }, [gameState, initGame, gameMode, showSettings]); // Re-bind on settings change for menu logic

  return (
    <div className="relative w-full h-screen flex justify-center items-center overflow-hidden bg-rose-50">
      <div className="relative shadow-2xl rounded-3xl overflow-hidden border-8 border-white/50" style={{ width: 800, height: 600 }}>
        <canvas ref={canvasRef} width={GAME_WIDTH} height={GAME_HEIGHT} className="w-full h-full object-cover" />

        {/* HUD - Players */}
        <div className="absolute top-6 left-6 flex flex-col gap-2">
            {playersState.map((p, i) => (
                <div key={i} className={`flex items-center gap-2 ${p.dead ? 'opacity-50 grayscale' : ''}`}>
                    <div className="bg-white/80 backdrop-blur-sm px-4 py-2 rounded-full shadow-sm border-2 border-pink-100 flex items-center gap-1">
                        <span className="font-bold text-xs text-gray-400 mr-2">{gameMode === 'COOP' ? `P${i+1}` : 'HP'}</span>
                        {[...Array(p.maxHp)].map((_, h) => (
                            <Heart key={h} size={20} className={`${h < p.hp ? 'fill-red-400 text-red-400' : 'fill-gray-200 text-gray-200'} transition-colors duration-300`} />
                        ))}
                    </div>
                    <div className={`transition-all duration-300 ${p.dashReady ? 'bg-blue-100 border-blue-200 scale-100' : 'bg-gray-100 border-gray-200 opacity-50 scale-90'} bg-white/80 backdrop-blur-sm p-2 rounded-full shadow-sm border-2 flex items-center justify-center`}>
                        <Zap size={16} className={p.dashReady ? "text-blue-500 fill-blue-500" : "text-gray-400"} />
                    </div>
                </div>
            ))}
        </div>

        {/* HUD - Score */}
        <div className="absolute top-6 right-6 flex flex-col gap-2 items-end">
             <div className="bg-white/80 backdrop-blur-sm px-6 py-2 rounded-full shadow-sm border-2 border-pink-100 flex items-center gap-2">
                <span className="text-yellow-400 font-bold text-xl drop-shadow-sm">●</span>
                <span className="text-pink-400 font-bold text-xl">{score.toString().padStart(6, '0')}</span>
             </div>
             <div className="bg-white/80 backdrop-blur-sm px-6 py-2 rounded-full shadow-sm border-2 border-pink-100 flex items-center gap-2">
                <Trophy size={20} className="text-yellow-400" />
                <span className="text-purple-400 font-bold text-xl">{highScore.toString().padStart(6, '0')}</span>
             </div>
        </div>

        {/* Boss Health Bar */}
        {bossActive && (
             <div className="absolute bottom-6 left-1/2 transform -translate-x-1/2 w-2/3 animate-fade-in-up">
                 <div className="flex justify-between text-purple-600 font-bold mb-1 px-2">
                     <span>BOSS: PATTERN #{ (stateRef.current.bossLevel % 10) + 1}</span>
                     <span>{bossHp}/{bossMaxHp}</span>
                 </div>
                 <div className="w-full h-6 bg-gray-200 rounded-full border-2 border-white overflow-hidden shadow-lg">
                     <div 
                        className="h-full bg-gradient-to-r from-purple-400 to-pink-500 transition-all duration-200"
                        style={{ width: `${(bossHp / bossMaxHp) * 100}%` }}
                     />
                 </div>
             </div>
        )}

        {/* Settings Modal */}
        {gameState === GameState.MENU && showSettings && (
             <div className="absolute inset-0 bg-black/40 backdrop-blur-md flex items-center justify-center z-50 animate-fade-in">
                 <div className="bg-white p-8 rounded-3xl shadow-2xl max-w-md w-full border-4 border-purple-100">
                     <div className="flex justify-between items-center mb-6">
                         <h2 className="text-3xl font-bold text-gray-700 flex items-center gap-2"><Settings className="text-purple-400" /> Settings</h2>
                         <button onClick={() => setShowSettings(false)} className="p-2 hover:bg-gray-100 rounded-full"><X /></button>
                     </div>
                     
                     <div className="space-y-6">
                         {/* Game Mode */}
                         <div className="bg-gray-50 p-4 rounded-xl">
                             <label className="flex items-center justify-between cursor-pointer">
                                 <span className="font-bold text-gray-600 text-lg">2 Player Co-op</span>
                                 <button 
                                    onClick={() => setGameMode(prev => prev === 'SINGLE' ? 'COOP' : 'SINGLE')}
                                    className={`w-14 h-8 rounded-full p-1 transition-colors duration-300 ${gameMode === 'COOP' ? 'bg-purple-500' : 'bg-gray-300'}`}
                                 >
                                     <div className={`bg-white w-6 h-6 rounded-full shadow-md transform transition-transform duration-300 ${gameMode === 'COOP' ? 'translate-x-6' : ''}`} />
                                 </button>
                             </label>
                             <p className="text-xs text-gray-400 mt-2">Team up! Enemy HP increased. Bosses are tougher.</p>
                         </div>
                         
                         {/* Gamepad Setup */}
                         <div className="bg-gray-50 p-4 rounded-xl">
                              <h3 className="font-bold text-gray-600 mb-3 flex items-center justify-between">
                                <span className="flex items-center gap-2"><Gamepad2 size={18}/> Gamepad Setup</span>
                                <span className={`text-[10px] px-2 py-1 rounded-full ${gamepadsDetected > 0 ? 'bg-green-100 text-green-700' : 'bg-red-100 text-red-700'}`}>
                                    {gamepadsDetected} Detected
                                </span>
                              </h3>
                              {gamepadConfigStep === 'none' && (
                                  <button 
                                    onClick={() => setGamepadConfigStep('p1_wait')}
                                    className="w-full py-2 bg-blue-100 text-blue-600 font-bold rounded-lg hover:bg-blue-200 transition-colors"
                                  >
                                      Connect Gamepads
                                  </button>
                              )}
                              
                              {(gamepadConfigStep === 'p1_wait' || gamepadConfigStep === 'p2_wait') && (
                                  <div className={`text-center p-2 rounded-lg animate-pulse font-bold ${gamepadConfigStep === 'p1_wait' ? 'bg-yellow-100 text-yellow-800' : 'bg-purple-100 text-purple-800'}`}>
                                      {gamepadConfigStep === 'p1_wait' ? 'Player 1' : 'Player 2'}: Press ZL + ZR
                                  </div>
                              )}
                              
                              {gamepadConfigStep === 'done' && (
                                  <button 
                                      onClick={() => {
                                          stateRef.current.gamepadAssignments = { p1: null, p2: null };
                                          setGamepadConfigStep('p1_wait');
                                      }}
                                      className="w-full text-center p-2 bg-green-100 hover:bg-green-200 text-green-800 rounded-lg flex items-center justify-center gap-2 font-bold transition-colors"
                                  >
                                      <CheckCircle2 size={16}/> Connected! (Click to Reset)
                                  </button>
                              )}
                              <p className="text-[10px] text-gray-400 mt-2 text-center">Switch Layout: ZL+ZR to pair. + for Menu. X Restart.</p>
                         </div>

                         {/* Controls View */}
                         <div className="bg-gray-50 p-4 rounded-xl">
                             <h3 className="font-bold text-gray-600 mb-3 flex items-center gap-2"><Keyboard size={18}/> Controls</h3>
                             <div className="grid grid-cols-2 gap-4 text-sm">
                                 <div>
                                     <p className="font-bold text-pink-500 mb-1">Player 1</p>
                                     <ul className="text-gray-500 space-y-1">
                                         <li>Move: <span className="font-mono bg-white px-1 rounded border">WASD</span></li>
                                         <li>Shoot: <span className="font-mono bg-white px-1 rounded border">J</span></li>
                                         <li>Dash: <span className="font-mono bg-white px-1 rounded border">K</span></li>
                                         <li>Heavy: <span className="font-mono bg-white px-1 rounded border">L</span></li>
                                     </ul>
                                 </div>
                                 <div>
                                     <p className="font-bold text-blue-500 mb-1">Player 2</p>
                                     <ul className="text-gray-500 space-y-1">
                                         <li>Move: <span className="font-mono bg-white px-1 rounded border">Arrows</span></li>
                                         <li>Shoot: <span className="font-mono bg-white px-1 rounded border">Num 1</span></li>
                                         <li>Dash: <span className="font-mono bg-white px-1 rounded border">Num 2</span></li>
                                         <li>Heavy: <span className="font-mono bg-white px-1 rounded border">Num 3</span></li>
                                     </ul>
                                 </div>
                             </div>
                         </div>
                     </div>
                 </div>
             </div>
        )}

        {/* Menu */}
        {gameState === GameState.MENU && !showSettings && (
            <div className="absolute inset-0 bg-white/60 backdrop-blur-sm flex flex-col items-center justify-center animate-fade-in">
                <h1 className="text-6xl font-black text-transparent bg-clip-text bg-gradient-to-r from-pink-400 to-purple-400 mb-2 drop-shadow-sm">Woolly Run</h1>
                <div className="text-gray-400 text-sm mb-6 flex items-center gap-2">
                    <span className="bg-gray-100 px-2 py-1 rounded text-xs font-bold">ESC / +</span> Settings
                </div>
                
                {/* Skin Selectors */}
                <div className="flex flex-col gap-6 mb-8">
                     {/* P1 Skins */}
                     <div className="flex flex-col items-center gap-2">
                         <span className="text-xs font-bold text-pink-400 uppercase tracking-widest">{gameMode === 'COOP' ? 'Player 1' : 'Select Character'}</span>
                         <div className="bg-white/80 p-3 rounded-2xl flex gap-3 shadow-sm border border-pink-100">
                            {SKINS.map((skin) => (
                                <button key={skin.id} onClick={() => setP1Skin(skin)} className={`relative w-12 h-12 rounded-lg transition-all ${p1Skin.id === skin.id ? 'ring-2 ring-pink-400 scale-110' : 'opacity-50 hover:opacity-100'}`} style={{ backgroundColor: skin.color }}>
                                    {p1Skin.id === skin.id && <User size={16} className="text-white absolute top-1/2 left-1/2 transform -translate-x-1/2 -translate-y-1/2" />}
                                </button>
                            ))}
                         </div>
                     </div>

                     {/* P2 Skins (Coop Only) */}
                     {gameMode === 'COOP' && (
                         <div className="flex flex-col items-center gap-2">
                             <span className="text-xs font-bold text-blue-400 uppercase tracking-widest">Player 2</span>
                             <div className="bg-white/80 p-3 rounded-2xl flex gap-3 shadow-sm border border-blue-100">
                                {SKINS.map((skin) => (
                                    <button key={skin.id} onClick={() => setP2Skin(skin)} className={`relative w-12 h-12 rounded-lg transition-all ${p2Skin.id === skin.id ? 'ring-2 ring-blue-400 scale-110' : 'opacity-50 hover:opacity-100'}`} style={{ backgroundColor: skin.color }}>
                                        {p2Skin.id === skin.id && <User size={16} className="text-white absolute top-1/2 left-1/2 transform -translate-x-1/2 -translate-y-1/2" />}
                                    </button>
                                ))}
                             </div>
                         </div>
                     )}
                </div>

                <button onClick={() => { initGame(); setGameState(GameState.PLAYING); }} className="px-10 py-5 bg-gradient-to-r from-pink-300 to-purple-300 rounded-full text-white font-bold text-2xl shadow-lg hover:shadow-xl hover:scale-105 transition-all flex items-center gap-3">
                    <Play className="fill-white" /> <span>{gameMode === 'COOP' ? 'Start Co-op' : 'Start Adventure'}</span>
                </button>
            </div>
        )}

        {/* Game Over */}
        {gameState === GameState.GAME_OVER && (
            <div className="absolute inset-0 bg-black/10 backdrop-blur-md flex flex-col items-center justify-center">
                <div className="bg-white/90 p-12 rounded-[2rem] shadow-2xl flex flex-col items-center text-center border-4 border-pink-200 animate-bounce-in">
                    <Skull size={48} className="text-pink-300 mb-4" />
                    <h2 className="text-5xl font-bold text-pink-500 mb-2">Oh no!</h2>
                    <p className="text-gray-400 mb-6">Press <span className="font-bold bg-gray-100 px-2 py-1 rounded">R</span> or <span className="font-bold bg-gray-100 px-2 py-1 rounded">X</span> to restart</p>
                    <div className="bg-yellow-50 p-4 rounded-xl mb-8 w-full">
                        <p className="text-sm text-yellow-600 font-bold uppercase tracking-wider">Score</p>
                        <p className="text-4xl font-black text-yellow-400">{score}</p>
                    </div>
                    <div className="flex gap-4">
                        <button onClick={() => setGameState(GameState.MENU)} className="px-6 py-4 bg-gray-100 hover:bg-gray-200 text-gray-600 rounded-full font-bold text-lg shadow-sm transition-all flex items-center gap-2"><Home size={20} /> Menu</button>
                        <button onClick={() => { initGame(); setGameState(GameState.PLAYING); }} className="px-8 py-4 bg-mint-400 bg-[#bbf7d0] hover:bg-[#86efac] text-green-700 rounded-full font-bold text-xl shadow-md hover:shadow-lg hover:-translate-y-1 transition-all flex items-center gap-2"><RefreshCcw size={24} /> Try Again</button>
                    </div>
                </div>
            </div>
        )}
      </div>
    </div>
  );
};
