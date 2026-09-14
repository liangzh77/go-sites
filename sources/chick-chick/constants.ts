

export const TILE_SIZE = 1;
export const PLAYABLE_WIDTH: number = 21; // Increased for wider play area
export const VISIBLE_WIDTH = 30; 
export const MOVEMENT_SPEED = 0.15;
export const JUMP_HEIGHT = 0.7;

export const SKINS = [
  { id: 'chicken', name: 'Chicken', price: 0, color: '#FFFFFF', model: 'chicken' },
  { id: 'pig', name: 'Piggy', price: 0, color: '#F48FB1', model: 'pig' },
  { id: 'penguin', name: 'Penguin', price: 0, color: '#222', model: 'penguin' },
  { id: 'bear', name: 'Polar Bear', price: 0, color: '#F0F0F0', model: 'bear' },
  { id: 'robot', name: 'Mech-Z', price: 25, color: '#78909C', model: 'robot' },
  { id: 'zombie', name: 'Rotter', price: 50, color: '#7CB342', model: 'zombie' },
  { id: 'vampire', name: 'Drac', price: 75, color: '#303030', model: 'vampire' },
  { id: 'golden', name: 'Midas', price: 100, color: '#FFD700', model: 'golden' },
];

export const BIOMES = [
  {
    name: 'PLAINS',
    colors: {
      GRASS: '#57D96A', GRASS_SIDE: '#3E9B4C', TREE: '#2D9E43', TREE_DARK: '#1E702D',
      ROAD: '#454A59', WATER: '#5FD4F7', WATER_DEEP: '#2AB8E0'
    }
  },
  {
    name: 'DESERT',
    colors: {
      GRASS: '#F4D03F', GRASS_SIDE: '#D4AC0D', TREE: '#B7950B', TREE_DARK: '#9A7D0A',
      ROAD: '#5D4037', WATER: '#2980B9', WATER_DEEP: '#1A5276'
    }
  },
  {
    name: 'SNOW',
    colors: {
      GRASS: '#EBF5FB', GRASS_SIDE: '#AED6F1', TREE: '#85C1E9', TREE_DARK: '#5DADE2',
      ROAD: '#2C3E50', WATER: '#1B4F72', WATER_DEEP: '#154360'
    }
  },
  {
    name: 'INFERNAL',
    colors: {
      GRASS: '#2C3E50', GRASS_SIDE: '#17202A', TREE: '#C0392B', TREE_DARK: '#922B21',
      ROAD: '#17202A', WATER: '#E74C3C', WATER_DEEP: '#943126' // Lava
    }
  }
];

export const STATIC_COLORS = {
  LOG: '#8D6E63',
  LOG_DARK: '#5D4037',
  ROCK: '#90A4AE',
  COIN: '#FFD700',
  RAIL: '#5D4037',
  RAIL_METAL: '#B0BEC5',
  TRAIN_BODY: '#C62828',
  TRAIN_STRIPE: '#FFEB3B'
};