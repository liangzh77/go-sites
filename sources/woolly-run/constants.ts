

export const GRAVITY = 0.5;
export const JUMP_FORCE = -18;
export const MOVE_SPEED = 6; 
export const FRICTION = 0.75; 
export const ACCELERATION = 2.5; 
export const GAME_WIDTH = 1200;
export const GAME_HEIGHT = 800;

// Dash Config
export const DASH_SPEED = 15;
export const DASH_DURATION = 10; // Frames
export const DASH_COOLDOWN = 60; // Frames (1 second at 60fps)

// Heavy Fall
export const HEAVY_FALL_SPEED = 25;

// Health & Combat Config
export const MAX_HEALTH = 3;
export const INVULNERABILITY_FRAMES = 90; // 1.5 Seconds at 60fps
export const ENEMY_HP = 5;
export const PLAYER_SHOOT_COOLDOWN = 20;

// Coop Config
export const COOP_ENEMY_HP = 7;
export const COOP_BOSS_HP_MULTIPLIER = 1.5;
export const COOP_BOSS_PROJ_MULTIPLIER = 1.5;

// Boss Config
export const BOSS_SCORE_THRESHOLD = 1500;
export const BOSS_HP_BASE = 30;
export const BOSS_HP_SCALING = 0; 

// Controls
export const CONTROLS = {
  P1: {
    UP: ['KeyW'],
    LEFT: ['KeyA'],
    RIGHT: ['KeyD'],
    DOWN: ['KeyS'],
    SHOOT: ['KeyJ'], // Updated to J
    DASH: ['KeyK'],  // Updated to K
    HEAVY: ['KeyL']  // Updated to L
  },
  P2: {
    UP: ['ArrowUp'],
    LEFT: ['ArrowLeft'],
    RIGHT: ['ArrowRight'],
    DOWN: ['ArrowDown'],
    SHOOT: ['Numpad1', 'KeyC'], // Fallbacks to prevent conflict
    DASH: ['Numpad2', 'KeyV'],
    HEAVY: ['Numpad3', 'KeyB']
  }
};

// Gamepad Config (Switch Pro / W3C Standard)
export const GAMEPAD_DEADZONE = 0.15;
export const GAMEPAD_MAP = {
  // Axes
  MOVE_X: 0, // Left Stick Horizontal
  SKIN_SELECT_X: 2, // Right Stick Horizontal
  
  // Buttons (Standard Mapping)
  JUMP: 0,   // B (Bottom)
  DASH: 1,   // A (Right) - Changed from 2
  RESTART: 3, // X (Top) - Corresponds to R key
  HEAVY: 6,  // ZL (Left Trigger) - Changed from 1
  ATTACK: 7, // ZR (Right Trigger)
  MENU: 9,   // + (Start) - Corresponds to ESC
  
  // Connection Combo
  CONNECT_L: 6, // ZL (Left Trigger)
  CONNECT_R: 7  // ZR (Right Trigger)
};

// Macaron Palette
export const COLORS = {
  sky: '#E0F7FA', // Soft Ice Blue
  clouds: '#FFFFFF',
  
  // Platform colors (Pastels)
  mint: '#B5EAD7',
  pink: '#FFB7B2',
  yellow: '#FFDAC1',
  purple: '#E2F0CB', 
  lavender: '#E0BBE4',
  movingPlat: '#C7CEEA', 
  arena: '#F8BBD0', // Special Boss Platform

  // Player defaults (overridden by skins)
  player: '#FF9AA2', 
  playerShadow: '#D87A81',

  // Items
  coin: '#FFD700', // Gold
  coinShadow: '#E6C200',
  
  // Enemies
  enemyPatrol: '#957DAD', // Muted Purple
  enemyFly: '#FF6961', // Muted Red
  enemyShooter: '#83BCA7', // Darker Mint
  enemyAura: '#FFB7B2', // Pinkish (Spiky)
  projectileEnemy: '#5D6D7E', // Dark Blue Grey
  projectilePlayer: '#FFB3BA', // Soft Pink Ball
  
  // Boss
  boss: '#C39BD3', // Purple Boss
  bossAngry: '#E74C3C', // Red when attacking

  text: '#685D79'
};

export const SKINS = [
  { id: 'berry', name: 'Berry', color: '#FF9AA2', shadow: '#D87A81' },
  { id: 'ice', name: 'Ice', color: '#A0E7E5', shadow: '#5DBEBC' },
  { id: 'lemon', name: 'Lemon', color: '#FBE7C6', shadow: '#D4B483' },
  { id: 'mint', name: 'Mint', color: '#B5EAD7', shadow: '#83BCA7' },
];

export const PLATFORM_CONFIG = {
  minWidth: 150,
  maxWidth: 350,
  minGap: 100,
  maxGap: 280,
  height: 40
};