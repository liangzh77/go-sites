
export enum GameState {
  MENU,
  PLAYING,
  GAME_OVER
}

export type GameMode = 'SINGLE' | 'COOP';

export interface Point {
  x: number;
  y: number;
}

export interface Size {
  width: number;
  height: number;
}

export interface Entity {
  id: string;
  x: number;
  y: number;
  width: number;
  height: number;
  color: string;
  type: 'player' | 'platform' | 'coin' | 'enemy' | 'cloud' | 'decoration' | 'projectile' | 'boss';
  subtype?: 'patrol' | 'fly' | 'moving' | 'static' | 'shooter' | 'aura' | 'boss_minion' | 'homing' | 'burst' | 'shotgun' | 'charged' | 'normal'; 
  vx?: number;
  vy?: number;
  startX?: number; 
  startY?: number;
  patrolRange?: number;
  shootTimer?: number; 
  markedForDeletion?: boolean;
  
  // Combat
  hp?: number;
  maxHp?: number;
  owner?: 'player' | 'enemy'; // For projectiles
  flashTimer?: number; // Visual feedback for taking damage
  damage?: number; // Projectile damage
  spawnTime?: number; // For burst timing

  // Boss Specific
  bossPattern?: number; // 0-9
  attackState?: 'idle' | 'warning' | 'attacking' | 'cooldown';
  attackTimer?: number;
  targetY?: number; // For sniper/homing logic
}

export interface Player extends Entity {
  playerIndex: number; // 0 or 1
  vx: number;
  vy: number;
  isGrounded: boolean;
  canDoubleJump: boolean;
  squashX: number;
  squashY: number;
  skinId: string;
  skinShadow: string;
  
  // States
  isDead: boolean;
  isDashing: boolean;
  dashTimer: number;
  dashCooldown: number;
  facingRight: boolean;

  // Heavy Fall
  isHeavyFalling: boolean;
  
  // Health properties
  health: number;
  maxHealth: number;
  invulnerable: boolean;
  invulnerabilityTimer: number;
  
  // Combat
  shootCooldown: number;
  chargeTimer: number; // For Mint skin
}

export interface Particle {
  id: string;
  x: number;
  y: number;
  vx: number;
  vy: number;
  life: number;
  color: string;
  size: number;
  type?: 'sparkle' | 'dust' | 'explosion';
}

export interface Camera {
  x: number;
  y: number;
}
