export enum LaneType {
  GRASS = 'GRASS',
  ROAD = 'ROAD',
  RIVER = 'RIVER',
  RAIL = 'RAIL',
  FOREST = 'FOREST'
}

export interface LaneData {
  id: number;
  index: number; // Z-index row
  type: LaneType;
  speed: number;
  density: number;
  direction: 1 | -1;
}

export interface Skin {
  id: string;
  name: string;
  price: number;
  color: string;
  model: 'chicken' | 'pig' | 'penguin' | 'bear' | 'robot' | 'zombie' | 'vampire' | 'golden';
}

export enum GameStatus {
  MENU = 'MENU',
  PLAYING = 'PLAYING',
  GAME_OVER = 'GAME_OVER',
  SHOP = 'SHOP'
}

export interface ScoreState {
  current: number;
  best: number;
}

export type DeathReason = 'FLATTENED' | 'DROWNED' | 'SMASHED' | 'LOST';